// Package fake is an in-memory Notion gateway used by sync tests and local
// development. It implements the same port as the HTTP client.
package fake

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/olianate/memo/internal/notion"
)

// Gateway is a deterministic, in-memory archive.
type Gateway struct {
	mu sync.Mutex

	DataSources map[string]*notion.DataSource
	Pages       map[string]*notion.Page
	Children    map[string][]notion.Block
	Uploads     map[string][]byte
	Downloads   map[string][]byte
	Users       map[string]*notion.User

	NextPage     int
	NextUpload   int
	NextBlock    int
	Calls        []string
	OnCall       func(method string) error
	QueryCount   int
	IncompleteAt int // when set, the Nth query returns the 10k truncation shape

	LastCreatePage notion.Page
}

// New returns an empty fake with one default user.
func New() *Gateway {
	return &Gateway{
		DataSources: map[string]*notion.DataSource{},
		Pages:       map[string]*notion.Page{},
		Children:    map[string][]notion.Block{},
		Uploads:     map[string][]byte{},
		Downloads:   map[string][]byte{},
		Users:       map[string]*notion.User{"me": {ID: "bot-1", Name: "Test Bot", WorkspaceName: "Test Workspace", MaxFileUpload: 5 << 20, BotType: "bot"}},
	}
}

func (g *Gateway) note(method string) error {
	g.Calls = append(g.Calls, method)
	if g.OnCall != nil {
		return g.OnCall(method)
	}
	return nil
}

// Me returns the configured user.
func (g *Gateway) Me(ctx context.Context) (*notion.User, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if err := g.note("Me"); err != nil {
		return nil, err
	}
	u := g.Users["me"]
	return u, nil
}

// SearchParentPages matches page and database titles and returns both kinds.
func (g *Gateway) SearchParentPages(ctx context.Context, query string) (*notion.ParentPageSearch, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if err := g.note("SearchParentPages"); err != nil {
		return nil, err
	}
	out := &notion.ParentPageSearch{Pages: []notion.Page{}, DataSources: []notion.DataSource{}}
	for _, p := range g.Pages {
		if p.InTrash || p.Parent.Type == "data_source_id" {
			continue
		}
		if query == "" || strings.Contains(strings.ToLower(notion.TitleFromPage(*p)), strings.ToLower(query)) {
			out.Pages = append(out.Pages, *p)
		}
	}
	for _, ds := range g.DataSources {
		if query == "" || strings.Contains(strings.ToLower(ds.Title), strings.ToLower(query)) {
			out.DataSources = append(out.DataSources, *ds)
		}
	}
	sort.Slice(out.Pages, func(i, j int) bool { return notion.TitleFromPage(out.Pages[i]) < notion.TitleFromPage(out.Pages[j]) })
	sort.Slice(out.DataSources, func(i, j int) bool { return out.DataSources[i].Title < out.DataSources[j].Title })
	return out, nil
}

// SearchTrash returns trashed pages.
func (g *Gateway) SearchTrash(ctx context.Context) ([]notion.Page, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if err := g.note("SearchTrash"); err != nil {
		return nil, err
	}
	out := []notion.Page{}
	for _, p := range g.Pages {
		if p.InTrash {
			out = append(out, *p)
		}
	}
	return out, nil
}

// CreateMemoDatabase creates the app-owned data source.
func (g *Gateway) CreateMemoDatabase(ctx context.Context, parentPageID, title string, properties map[string]notion.PropertySchema) (*notion.DataSource, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if err := g.note("CreateMemoDatabase"); err != nil {
		return nil, err
	}
	id := "ds-1"
	types := map[string]string{}
	for name, schema := range properties {
		switch {
		case schema.Title != nil:
			types[name] = "title"
		case schema.RichText != nil:
			types[name] = "rich_text"
		case schema.Date != nil:
			types[name] = "date"
		case schema.Number != nil:
			types[name] = "number"
		case schema.Select != nil:
			types[name] = "select"
		case schema.MultiSelect != nil:
			types[name] = "multi_select"
		}
	}
	ds := &notion.DataSource{ID: id, DatabaseID: "db-1", Title: title, Properties: properties,
		PropertyTypes: types, Options: map[string][]notion.Option{}}
	for name, schema := range properties {
		if schema.MultiSelect != nil {
			ds.Options[name] = append(ds.Options[name], schema.MultiSelect.Options...)
		}
		if schema.Select != nil {
			ds.Options[name] = append(ds.Options[name], schema.Select.Options...)
		}
	}
	g.DataSources[id] = ds
	return ds, nil
}

// ResolveDataSource accepts a data source ID or a database ID.
func (g *Gateway) ResolveDataSource(ctx context.Context, id string) (*notion.DataSource, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if err := g.note("ResolveDataSource"); err != nil {
		return nil, err
	}
	if ds, ok := g.DataSources[id]; ok {
		return ds, nil
	}
	for _, ds := range g.DataSources {
		if ds.DatabaseID == id {
			return ds, nil
		}
	}
	return nil, notion.StatusError(404, "object_not_found", "no data source or database "+id)
}

// RetrieveDataSource returns the schema.
func (g *Gateway) RetrieveDataSource(ctx context.Context, id string) (*notion.DataSource, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if err := g.note("RetrieveDataSource"); err != nil {
		return nil, err
	}
	ds, ok := g.DataSources[id]
	if !ok {
		return nil, notion.StatusError(404, "object_not_found", "no data source "+id)
	}
	return ds, nil
}

// UpdateDataSourceSchema merges property types.
func (g *Gateway) UpdateDataSourceSchema(ctx context.Context, id string, properties map[string]notion.PropertySchema) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	if err := g.note("UpdateDataSourceSchema"); err != nil {
		return err
	}
	ds, ok := g.DataSources[id]
	if !ok {
		return notion.StatusError(404, "object_not_found", "no data source "+id)
	}
	if ds.PropertyTypes == nil {
		ds.PropertyTypes = map[string]string{}
	}
	if ds.Options == nil {
		ds.Options = map[string][]notion.Option{}
	}
	for name, schema := range properties {
		switch {
		case schema.Title != nil:
			ds.PropertyTypes[name] = "title"
		case schema.MultiSelect != nil:
			ds.PropertyTypes[name] = "multi_select"
			ds.Options[name] = mergeOptions(ds.Options[name], schema.MultiSelect.Options)
		case schema.Select != nil:
			ds.PropertyTypes[name] = "select"
			ds.Options[name] = mergeOptions(ds.Options[name], schema.Select.Options)
		case schema.Number != nil:
			ds.PropertyTypes[name] = "number"
		case schema.Date != nil:
			ds.PropertyTypes[name] = "date"
		default:
			ds.PropertyTypes[name] = "rich_text"
		}
	}
	return nil
}

func mergeOptions(existing, added []notion.Option) []notion.Option {
	out := append([]notion.Option{}, existing...)
	seen := map[string]bool{}
	for _, o := range out {
		seen[o.Name] = true
	}
	for _, o := range added {
		if !seen[o.Name] {
			out = append(out, o)
			seen[o.Name] = true
		}
	}
	return out
}

// QueryDataSource filters, sorts and paginates pages.
func (g *Gateway) QueryDataSource(ctx context.Context, id string, req notion.QueryRequest) (*notion.QueryResponse, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if err := g.note("QueryDataSource"); err != nil {
		return nil, err
	}
	if _, ok := g.DataSources[id]; !ok {
		return nil, notion.StatusError(404, "object_not_found", "no data source "+id)
	}
	g.QueryCount++
	var rows []notion.Page
	for _, p := range g.Pages {
		if p.InTrash {
			continue
		}
		if !matchFilter(*p, req.Filter) {
			continue
		}
		rows = append(rows, *p)
	}
	sort.Slice(rows, func(i, j int) bool {
		dir := 1
		if len(req.Sorts) > 0 && req.Sorts[0].Direction == "descending" {
			dir = -1
		}
		var a, b time.Time
		if len(req.Sorts) > 0 && req.Sorts[0].Timestamp == "created_time" {
			a, b = rows[i].CreatedTime, rows[j].CreatedTime
		} else {
			a, b = notion.UpdatedAtFromPage(rows[i]), notion.UpdatedAtFromPage(rows[j])
		}
		if a.Equal(b) {
			return rows[i].ID < rows[j].ID
		}
		return a.Before(b) == (dir == 1)
	})

	if g.IncompleteAt > 0 && g.QueryCount == g.IncompleteAt {
		return &notion.QueryResponse{
			Results:       rows,
			HasMore:       false,
			RequestStatus: notion.RequestStatus{Type: "incomplete", IncompleteReason: "query_result_limit_reached"},
		}, nil
	}

	pageSize := req.PageSize
	if pageSize <= 0 || pageSize > 100 {
		pageSize = 100
	}
	offset := 0
	if req.StartCursor != "" {
		offset, _ = strconv.Atoi(req.StartCursor)
	}
	end := offset + pageSize
	if end > len(rows) {
		end = len(rows)
	}
	resp := &notion.QueryResponse{Results: []notion.Page{}}
	if offset < len(rows) {
		resp.Results = rows[offset:end]
	}
	if end < len(rows) {
		resp.HasMore = true
		resp.NextCursor = strconv.Itoa(end)
	}
	return resp, nil
}

func matchFilter(p notion.Page, filter map[string]any) bool {
	if filter == nil {
		return true
	}
	if ts, ok := filter["timestamp"].(string); ok {
		cond, _ := filter[ts].(map[string]any)
		if cond != nil {
			start, _ := cond["on_or_after"].(string)
			if start != "" {
				t, err := time.Parse(time.RFC3339, start)
				if err == nil {
					var actual time.Time
					if ts == "created_time" {
						actual = p.CreatedTime
					} else {
						actual = notion.UpdatedAtFromPage(p)
					}
					if actual.Before(t) {
						return false
					}
				}
			}
		}
		return true
	}
	prop, _ := filter["property"].(string)
	switch prop {
	case notion.PropMemoID:
		cond, _ := filter["rich_text"].(map[string]any)
		equals, _ := cond["equals"].(string)
		return notion.MemoIDFromPage(p) == equals
	case notion.PropUpdated, notion.PropCreated:
		cond, _ := filter["date"].(map[string]any)
		start, _ := cond["on_or_after"].(string)
		if start == "" {
			return true
		}
		t, err := time.Parse(time.RFC3339, start)
		if err != nil {
			return true
		}
		actual := notion.UpdatedAtFromPage(p)
		if prop == notion.PropCreated {
			actual = notion.CreatedAtFromPage(p)
		}
		return !actual.Before(t)
	case notion.PropStatus:
		cond, _ := filter["select"].(map[string]any)
		equals, _ := cond["equals"].(string)
		if v, ok := p.Properties[notion.PropStatus]; ok && v.Select != nil {
			return v.Select.Name == equals
		}
		return false
	}
	return true
}

// CreatePage stores a page and its children.
func (g *Gateway) CreatePage(ctx context.Context, dataSourceID string, properties map[string]notion.PropertyValue, children []notion.Block) (*notion.Page, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if err := g.note("CreatePage"); err != nil {
		return nil, err
	}
	g.NextPage++
	pageID := fmt.Sprintf("page-%d", g.NextPage)
	now := time.Now().UTC()
	page := &notion.Page{ID: pageID, CreatedTime: now, LastEditedTime: now, Properties: properties}
	g.Pages[pageID] = page
	g.Children[pageID] = g.assignBlocks(children)
	g.LastCreatePage = *page
	out := *page
	return &out, nil
}

func (g *Gateway) assignBlocks(blocks []notion.Block) []notion.Block {
	out := make([]notion.Block, len(blocks))
	for i, b := range blocks {
		g.NextBlock++
		b.ID = fmt.Sprintf("block-%d", g.NextBlock)
		b.Object = "block"
		// Notion resolves a file_upload reference into a hosted file object and
		// returns a signed URL when the block is read back.
		if b.Type == "image" && b.Image != nil && b.Image.FileUpload != nil {
			url := g.attachUpload(b.Image.FileUpload.ID)
			b.Image = &notion.FileBlock{Type: "file", File: &notion.FileObject{URL: url}, Caption: b.Image.Caption}
		}
		if b.Type == "file" && b.File != nil && b.File.FileUpload != nil {
			url := g.attachUpload(b.File.FileUpload.ID)
			b.File = &notion.FileBlock{Type: "file", File: &notion.FileObject{URL: url}, Caption: b.File.Caption}
		}
		out[i] = b
	}
	return out
}

func (g *Gateway) attachUpload(uploadID string) string {
	if uploadID == "" {
		return ""
	}
	url := "https://signed.example/" + uploadID
	g.Downloads[url] = g.Uploads[uploadID]
	return url
}

// UpdatePage merges properties.
func (g *Gateway) UpdatePage(ctx context.Context, pageID string, properties map[string]notion.PropertyValue) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	if err := g.note("UpdatePage"); err != nil {
		return err
	}
	p, ok := g.Pages[pageID]
	if !ok {
		return notion.StatusError(404, "object_not_found", "no page "+pageID)
	}
	if p.Properties == nil {
		p.Properties = map[string]notion.PropertyValue{}
	}
	for k, v := range properties {
		p.Properties[k] = v
	}
	p.LastEditedTime = time.Now().UTC()
	return nil
}

// GetPage returns a page, including trashed pages.
func (g *Gateway) GetPage(ctx context.Context, pageID string) (*notion.Page, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if err := g.note("GetPage"); err != nil {
		return nil, err
	}
	p, ok := g.Pages[pageID]
	if !ok {
		return nil, notion.StatusError(404, "object_not_found", "no page "+pageID)
	}
	out := *p
	return &out, nil
}

// ListChildren paginates a block's children.
func (g *Gateway) ListChildren(ctx context.Context, blockID, cursor string, pageSize int) (*notion.ChildrenResponse, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if err := g.note("ListChildren"); err != nil {
		return nil, err
	}
	blocks := g.Children[blockID]
	offset := 0
	if cursor != "" {
		offset, _ = strconv.Atoi(cursor)
	}
	if pageSize <= 0 || pageSize > 100 {
		pageSize = 100
	}
	end := offset + pageSize
	if end > len(blocks) {
		end = len(blocks)
	}
	resp := &notion.ChildrenResponse{Results: []notion.Block{}}
	if offset < len(blocks) {
		resp.Results = blocks[offset:end]
	}
	if end < len(blocks) {
		resp.HasMore = true
		resp.NextCursor = strconv.Itoa(end)
	}
	return resp, nil
}

// AppendChildren appends to a page.
func (g *Gateway) AppendChildren(ctx context.Context, blockID string, children []notion.Block) (*notion.ChildrenResponse, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if err := g.note("AppendChildren"); err != nil {
		return nil, err
	}
	assigned := g.assignBlocks(children)
	g.Children[blockID] = append(g.Children[blockID], assigned...)
	return &notion.ChildrenResponse{Results: assigned}, nil
}

// DeleteBlock removes one child.
func (g *Gateway) DeleteBlock(ctx context.Context, blockID string) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	if err := g.note("DeleteBlock"); err != nil {
		return err
	}
	for parent, blocks := range g.Children {
		for i, b := range blocks {
			if b.ID == blockID {
				g.Children[parent] = append(blocks[:i], blocks[i+1:]...)
				return nil
			}
		}
	}
	return nil
}

// UploadFile stores the bytes and returns an upload id.
func (g *Gateway) UploadFile(ctx context.Context, filename, contentType string, data []byte) (string, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if err := g.note("UploadFile"); err != nil {
		return "", err
	}
	g.NextUpload++
	id := fmt.Sprintf("upload-%d", g.NextUpload)
	g.Uploads[id] = data
	return id, nil
}

// Download resolves a URL previously registered in Downloads.
func (g *Gateway) Download(ctx context.Context, url string) ([]byte, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if err := g.note("Download"); err != nil {
		return nil, err
	}
	data, ok := g.Downloads[url]
	if !ok {
		return nil, notion.StatusError(404, "object_not_found", "no file at "+url)
	}
	return data, nil
}

// SeedDataSource inserts a shared database for onboarding tests.
func (g *Gateway) SeedDataSource(ds notion.DataSource) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if ds.DatabaseID == "" {
		ds.DatabaseID = "db-" + ds.ID
	}
	if ds.PropertyTypes == nil {
		ds.PropertyTypes = map[string]string{}
	}
	if ds.Options == nil {
		ds.Options = map[string][]notion.Option{}
	}
	g.DataSources[ds.ID] = &ds
}

// SeedPage inserts a page with children for pull tests.
func (g *Gateway) SeedPage(page notion.Page, children []notion.Block) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if page.ID == "" {
		g.NextPage++
		page.ID = fmt.Sprintf("page-%d", g.NextPage)
	}
	g.Pages[page.ID] = &page
	g.Children[page.ID] = g.assignBlocks(children)
}

// Trash marks a page as manually trashed (for the sweep test).
func (g *Gateway) Trash(pageID string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if p, ok := g.Pages[pageID]; ok {
		p.InTrash = true
	}
}

// CallsOf returns how many times a method was invoked.
func (g *Gateway) CallsOf(method string) int {
	g.mu.Lock()
	defer g.mu.Unlock()
	n := 0
	for _, c := range g.Calls {
		if c == method {
			n++
		}
	}
	return n
}
