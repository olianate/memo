// Package mcpserver exposes the memo library over the Model Context Protocol
// using the Streamable HTTP transport. It listens on the loopback interface
// only: MCP clients must run on the same machine as the app.
package mcpserver

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"mime"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	mcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/olianate/memo/internal/domain/markdown"
	"github.com/olianate/memo/internal/memoedit"
	"github.com/olianate/memo/internal/services"
)

const endpointPath = "/mcp"

const (
	maxAttachmentsPerCall = 20
	maxAttachmentBytes    = 64 << 20
)

// MemoAPI is the subset of the memo service the MCP tools need for reads and
// deletion. Writes go through the shared editor.
type MemoAPI interface {
	Get(id string) (services.MemoDTO, error)
	Delete(id string) error
}

// SearchAPI is the subset of the search service the MCP tools need.
type SearchAPI interface {
	Query(query, tag, from, to string) ([]services.SearchResultDTO, error)
}

// Service owns the MCP server and its HTTP listener.
type Service struct {
	memo   MemoAPI
	editor *memoedit.Editor
	search SearchAPI
	server *mcp.Server

	mu       sync.Mutex
	http     *http.Server
	port     int
	running  bool
	endpoint string
	lastErr  string
}

// New builds the MCP service and registers its tools.
func New(memo MemoAPI, editor *memoedit.Editor, search SearchAPI) *Service {
	s := &Service{memo: memo, editor: editor, search: search}
	s.server = mcp.NewServer(&mcp.Implementation{Name: "memo", Version: services.Version}, nil)
	registerTools(s)
	return s
}

// Start serves MCP on 127.0.0.1:port. Port 0 asks the OS for a free port and is
// intended for tests. Starting an already-running server on the same port is a
// no-op; a different port replaces the listener.
func (s *Service) Start(port int) error {
	if port < 0 || port > 65535 {
		return errors.New("MCP 端口需在 0 到 65535 之间")
	}
	s.mu.Lock()
	if s.running && s.port == port {
		s.mu.Unlock()
		return nil
	}
	if s.http != nil {
		s.mu.Unlock()
		if err := s.Stop(); err != nil {
			return err
		}
		s.mu.Lock()
	}

	ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		s.lastErr = err.Error()
		s.mu.Unlock()
		return fmt.Errorf("启动 MCP 服务失败: %w", err)
	}
	actualPort := ln.Addr().(*net.TCPAddr).Port

	mux := http.NewServeMux()
	mux.Handle(endpointPath, mcp.NewStreamableHTTPHandler(
		func(*http.Request) *mcp.Server { return s.server },
		&mcp.StreamableHTTPOptions{
			Stateless:      true,
			JSONResponse:   true,
			SessionTimeout: 30 * time.Minute,
		},
	))
	hs := &http.Server{Handler: mux, ReadHeaderTimeout: 10 * time.Second}
	s.http = hs
	s.port = actualPort
	s.running = true
	s.endpoint = fmt.Sprintf("http://127.0.0.1:%d%s", actualPort, endpointPath)
	s.lastErr = ""
	s.mu.Unlock()

	go func() {
		err := hs.Serve(ln)
		if err == nil || errors.Is(err, http.ErrServerClosed) {
			return
		}
		s.mu.Lock()
		// Only the listener that actually failed should change state.
		if s.http == hs {
			s.running = false
			s.http = nil
			s.endpoint = ""
			s.lastErr = err.Error()
		}
		s.mu.Unlock()
	}()
	return nil
}

// Stop gracefully shuts the HTTP server down.
func (s *Service) Stop() error {
	s.mu.Lock()
	hs := s.http
	s.http = nil
	s.running = false
	s.endpoint = ""
	s.lastErr = ""
	s.mu.Unlock()
	if hs == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := hs.Shutdown(ctx); err != nil {
		_ = hs.Close()
		return err
	}
	return nil
}

// Status reports whether the server is accepting requests.
func (s *Service) Status() (running bool, endpoint string, lastErr string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.running, s.endpoint, s.lastErr
}

// MemoRecord is the MCP-facing view of a memo.
type MemoRecord struct {
	ID          string             `json:"id"`
	CreatedAt   string             `json:"created_at"`
	UpdatedAt   string             `json:"updated_at"`
	Content     string             `json:"content"`
	Tags        []string           `json:"tags"`
	Attachments []AttachmentRecord `json:"attachments"`
	Links       []string           `json:"links"`
	Deleted     bool               `json:"deleted"`
	LocalState  string             `json:"local_state"`
	Snippet     string             `json:"snippet"`
	Day         string             `json:"day"`
}

// AttachmentRecord describes an image or file attached to a memo.
type AttachmentRecord struct {
	ID      string `json:"id"`
	Kind    string `json:"kind"`
	Name    string `json:"name"`
	MIME    string `json:"mime"`
	Bytes   int64  `json:"bytes"`
	Width   int    `json:"width,omitempty"`
	Height  int    `json:"height,omitempty"`
	Present bool   `json:"present"`
}

// attachmentInput lets an MCP client attach a local file by path or inline
// base64. Images are detected by MIME type and rendered as images.
type attachmentInput struct {
	Path       string `json:"path,omitempty" jsonschema:"本机文件路径；与 data_base64 二选一"`
	DataBase64 string `json:"data_base64,omitempty" jsonschema:"文件内容的 base64；与 path 二选一"`
	Name       string `json:"name,omitempty" jsonschema:"文件名；path 模式可省略，base64 模式建议提供"`
	MIME       string `json:"mime,omitempty" jsonschema:"MIME 类型，例如 image/png；省略时按扩展名或内容自动识别"`
}

type loadedAttachment struct {
	Name string
	MIME string
	Data []byte
}

type searchMemosInput struct {
	Query string `json:"query" jsonschema:"搜索关键词，必填"`
	Tag   string `json:"tag,omitempty" jsonschema:"按标签精确过滤"`
	From  string `json:"from,omitempty" jsonschema:"起始时间，RFC3339，例如 2026-01-01T00:00:00+08:00"`
	To    string `json:"to,omitempty" jsonschema:"结束时间，RFC3339"`
	Limit int    `json:"limit,omitempty" jsonschema:"最多返回条数，默认 20，最大 100"`
}

type searchHit struct {
	Memo   MemoRecord `json:"memo"`
	Before string     `json:"before"`
	Match  string     `json:"match"`
	After  string     `json:"after"`
}

type searchMemosOutput struct {
	Count   int         `json:"count"`
	Results []searchHit `json:"results"`
}

type createMemoInput struct {
	Content     string            `json:"content,omitempty" jsonschema:"备忘录内容，Markdown 格式；可只传附件"`
	Tags        []string          `json:"tags,omitempty" jsonschema:"标签列表，不含 # 前缀"`
	Attachments []attachmentInput `json:"attachments,omitempty" jsonschema:"要附加的图片或文件，可用本机路径或 base64"`
}

type updateMemoInput struct {
	ID                  string            `json:"id" jsonschema:"要修改的备忘录 ID，必填"`
	Content             string            `json:"content,omitempty" jsonschema:"新的 Markdown 内容；省略则保留原正文"`
	Tags                []string          `json:"tags,omitempty" jsonschema:"新的标签列表；省略则保留原标签，传空数组则清空标签"`
	Attachments         []attachmentInput `json:"attachments,omitempty" jsonschema:"要追加的图片或文件"`
	RemoveAttachmentIDs []string          `json:"remove_attachment_ids,omitempty" jsonschema:"要移除的附件 ID 列表"`
}

type deleteMemoInput struct {
	ID string `json:"id" jsonschema:"要删除的备忘录 ID，必填"`
}

type memoOutput struct {
	Memo MemoRecord `json:"memo"`
}

type deleteMemoOutput struct {
	ID      string `json:"id"`
	Deleted bool   `json:"deleted"`
}

func registerTools(s *Service) {
	mcp.AddTool(s.server, &mcp.Tool{
		Name:        "search_memos",
		Description: "搜索备忘录。可按关键词、标签和创建时间范围过滤，返回匹配片段。",
	}, s.searchMemos)
	mcp.AddTool(s.server, &mcp.Tool{
		Name:        "create_memo",
		Description: "新建一条备忘录。内容使用 Markdown，可附带标签、本机文件路径或 base64 图片/附件。内容中的 [文字](链接) 会保存为可点击链接。",
	}, s.createMemo)
	mcp.AddTool(s.server, &mcp.Tool{
		Name:        "update_memo",
		Description: "修改一条备忘录的内容、标签或附件。省略的字段保持不变；可用 remove_attachment_ids 移除附件，新附件会追加到正文末尾。",
	}, s.updateMemo)
	mcp.AddTool(s.server, &mcp.Tool{
		Name:        "delete_memo",
		Description: "删除一条备忘录（移入回收站，可恢复）。",
	}, s.deleteMemo)
}

func (s *Service) searchMemos(_ context.Context, _ *mcp.CallToolRequest, in searchMemosInput) (*mcp.CallToolResult, searchMemosOutput, error) {
	query := strings.TrimSpace(in.Query)
	if query == "" {
		return nil, searchMemosOutput{}, errors.New("query 不能为空")
	}
	limit := in.Limit
	if limit <= 0 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}
	results, err := s.search.Query(query, in.Tag, in.From, in.To)
	if err != nil {
		return nil, searchMemosOutput{}, err
	}
	if len(results) > limit {
		results = results[:limit]
	}
	out := searchMemosOutput{Count: len(results), Results: make([]searchHit, 0, len(results))}
	for _, r := range results {
		out.Results = append(out.Results, searchHit{
			Memo:   toRecord(r.Memo),
			Before: r.Before,
			Match:  r.Match,
			After:  r.After,
		})
	}
	return nil, out, nil
}

func (s *Service) createMemo(ctx context.Context, _ *mcp.CallToolRequest, in createMemoInput) (*mcp.CallToolResult, memoOutput, error) {
	content := strings.TrimSpace(in.Content)
	if content == "" && len(in.Attachments) == 0 {
		return nil, memoOutput{}, errors.New("content 和 attachments 至少提供一项")
	}
	loaded, err := loadAttachments(in.Attachments)
	if err != nil {
		return nil, memoOutput{}, err
	}
	memo, err := s.editor.Create(ctx, memoedit.Draft{
		Body:  in.Content,
		Tags:  in.Tags,
		Files: filesFrom(loaded),
	})
	if err != nil {
		return nil, memoOutput{}, err
	}
	current, err := s.memo.Get(memo.ID)
	if err != nil {
		return nil, memoOutput{}, err
	}
	return nil, memoOutput{Memo: toRecord(current)}, nil
}

func (s *Service) updateMemo(ctx context.Context, _ *mcp.CallToolRequest, in updateMemoInput) (*mcp.CallToolResult, memoOutput, error) {
	id := strings.TrimSpace(in.ID)
	if id == "" {
		return nil, memoOutput{}, errors.New("id 不能为空")
	}
	hasContent := strings.TrimSpace(in.Content) != ""
	if !hasContent && in.Tags == nil && len(in.Attachments) == 0 && len(in.RemoveAttachmentIDs) == 0 {
		return nil, memoOutput{}, errors.New("content、tags、attachments 和 remove_attachment_ids 至少提供一项")
	}
	loaded, err := loadAttachments(in.Attachments)
	if err != nil {
		return nil, memoOutput{}, err
	}
	change := memoedit.Change{
		AddFiles:            filesFrom(loaded),
		RemoveAttachmentIDs: in.RemoveAttachmentIDs,
	}
	if hasContent {
		change.Body = &in.Content
		// A content-only edit must not silently drop existing images/files.
		// Explicit removals still win because they are stripped afterwards.
		change.PreserveAttachments = true
	}
	if in.Tags != nil {
		tags := in.Tags
		change.Tags = &tags
	}
	memo, err := s.editor.Edit(ctx, id, change)
	if err != nil {
		return nil, memoOutput{}, err
	}
	current, err := s.memo.Get(memo.ID)
	if err != nil {
		return nil, memoOutput{}, err
	}
	return nil, memoOutput{Memo: toRecord(current)}, nil
}

func loadAttachments(inputs []attachmentInput) ([]loadedAttachment, error) {
	if len(inputs) > maxAttachmentsPerCall {
		return nil, fmt.Errorf("一次最多添加 %d 个附件", maxAttachmentsPerCall)
	}
	out := make([]loadedAttachment, 0, len(inputs))
	for i, in := range inputs {
		path := strings.TrimSpace(in.Path)
		encoded := strings.TrimSpace(in.DataBase64)
		if (path == "") == (encoded == "") {
			return nil, fmt.Errorf("第 %d 个附件必须且只能提供 path 或 data_base64 之一", i+1)
		}
		var (
			data []byte
			name = strings.TrimSpace(in.Name)
			err  error
		)
		if path != "" {
			info, statErr := os.Stat(path)
			if statErr != nil {
				return nil, fmt.Errorf("读取附件 %s 失败: %w", path, statErr)
			}
			if info.IsDir() {
				return nil, fmt.Errorf("附件路径是目录: %s", path)
			}
			if info.Size() > maxAttachmentBytes {
				return nil, fmt.Errorf("附件 %s 超过 %d MiB 限制", path, maxAttachmentBytes>>20)
			}
			data, err = os.ReadFile(path)
			if err != nil {
				return nil, fmt.Errorf("读取附件 %s 失败: %w", path, err)
			}
			if name == "" {
				name = filepath.Base(path)
			}
		} else {
			if comma := strings.Index(encoded, ","); strings.HasPrefix(encoded, "data:") && comma >= 0 {
				encoded = encoded[comma+1:]
			}
			data, err = base64.StdEncoding.DecodeString(encoded)
			if err != nil {
				data, err = base64.RawStdEncoding.DecodeString(encoded)
			}
			if err != nil {
				return nil, fmt.Errorf("第 %d 个附件的 data_base64 无效", i+1)
			}
			if name == "" {
				name = "attachment"
			}
		}
		if len(data) == 0 {
			return nil, fmt.Errorf("第 %d 个附件内容为空", i+1)
		}
		if len(data) > maxAttachmentBytes {
			return nil, fmt.Errorf("第 %d 个附件超过 %d MiB 限制", i+1, maxAttachmentBytes>>20)
		}
		mimeType := strings.TrimSpace(in.MIME)
		if mimeType == "" {
			mimeType = mimeTypeFor(name, data)
		}
		out = append(out, loadedAttachment{Name: name, MIME: mimeType, Data: data})
	}
	return out, nil
}

func mimeTypeFor(name string, data []byte) string {
	if ext := filepath.Ext(name); ext != "" {
		if value := mime.TypeByExtension(ext); value != "" {
			return value
		}
	}
	return http.DetectContentType(data)
}

func filesFrom(items []loadedAttachment) []memoedit.File {
	out := make([]memoedit.File, 0, len(items))
	for _, item := range items {
		out = append(out, memoedit.File{Name: item.Name, MIME: item.MIME, Data: item.Data})
	}
	return out
}

func (s *Service) deleteMemo(_ context.Context, _ *mcp.CallToolRequest, in deleteMemoInput) (*mcp.CallToolResult, deleteMemoOutput, error) {
	id := strings.TrimSpace(in.ID)
	if id == "" {
		return nil, deleteMemoOutput{}, errors.New("id 不能为空")
	}
	if err := s.memo.Delete(id); err != nil {
		return nil, deleteMemoOutput{}, err
	}
	return nil, deleteMemoOutput{ID: id, Deleted: true}, nil
}

func toRecord(m services.MemoDTO) MemoRecord {
	tags := make([]string, 0, len(m.Tags))
	for _, t := range m.Tags {
		tags = append(tags, t.Name)
	}
	attachments := make([]AttachmentRecord, 0, len(m.Attachments))
	for _, a := range m.Attachments {
		attachments = append(attachments, AttachmentRecord{
			ID: a.ID, Kind: a.Kind, Name: a.Name, MIME: a.MIME,
			Bytes: a.Bytes, Width: a.Width, Height: a.Height, Present: a.Present,
		})
	}
	links := make([]string, 0)
	seenLinks := make(map[string]bool)
	for _, block := range markdown.Parse(m.Body) {
		for _, span := range block.Spans {
			link := strings.TrimSpace(span.Link)
			if link == "" || seenLinks[link] {
				continue
			}
			seenLinks[link] = true
			links = append(links, link)
		}
	}
	return MemoRecord{
		ID:          m.ID,
		CreatedAt:   m.CreatedAt,
		UpdatedAt:   m.UpdatedAt,
		Content:     m.Body,
		Tags:        tags,
		Attachments: attachments,
		Links:       links,
		Deleted:     m.Deleted,
		LocalState:  m.LocalState,
		Snippet:     m.Snippet,
		Day:         m.Day,
	}
}
