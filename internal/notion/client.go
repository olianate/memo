package notion

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

const defaultBaseURL = "https://api.notion.com/v1"

// Client is the HTTP adapter for Gateway.
type Client struct {
	token        string
	baseURL      string
	http         *http.Client
	rateInterval time.Duration
	limiter      *slotLimiter
	maxRetries   int
}

// ClientOption customizes a Client. Tests use BaseURL and RateInterval=0.
type ClientOption func(*Client)

// WithBaseURL points the client at a test server.
func WithBaseURL(base string) ClientOption {
	return func(c *Client) { c.baseURL = strings.TrimRight(base, "/") }
}

// WithRateInterval sets the minimum gap between requests (default 1/3s).
func WithRateInterval(d time.Duration) ClientOption {
	return func(c *Client) { c.rateInterval = d }
}

// WithHTTPClient injects an HTTP client.
func WithHTTPClient(h *http.Client) ClientOption {
	return func(c *Client) { c.http = h }
}

// NewClient returns a Notion gateway for a personal access token.
func NewClient(token string, opts ...ClientOption) *Client {
	c := &Client{
		token:        token,
		baseURL:      defaultBaseURL,
		http:         newSafeHTTPClient(),
		rateInterval: time.Second / 3,
		maxRetries:   2,
	}
	for _, opt := range opts {
		opt(c)
	}
	c.limiter = newSlotLimiter(c.rateInterval)
	return c
}

type slotLimiter struct {
	mu       sync.Mutex
	interval time.Duration
	next     time.Time
}

func newSlotLimiter(interval time.Duration) *slotLimiter { return &slotLimiter{interval: interval} }

func (l *slotLimiter) wait(ctx context.Context) error {
	if l.interval <= 0 {
		return nil
	}
	l.mu.Lock()
	now := time.Now()
	if l.next.Before(now) {
		l.next = now
	}
	slot := l.next
	l.next = l.next.Add(l.interval)
	l.mu.Unlock()
	delay := time.Until(slot)
	if delay <= 0 {
		return nil
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// Me validates the token and reports workspace limits.
func (c *Client) Me(ctx context.Context) (*User, error) {
	var raw struct {
		ID   string `json:"id"`
		Name string `json:"name"`
		Type string `json:"type"`
		Bot  struct {
			Owner struct {
				Workspace bool `json:"workspace"`
			} `json:"owner"`
			WorkspaceName   string `json:"workspace_name"`
			WorkspaceLimits struct {
				MaxFileUpload int64 `json:"max_file_upload_size_in_bytes"`
			} `json:"workspace_limits"`
		} `json:"bot"`
	}
	if err := c.do(ctx, http.MethodGet, "/users/me", nil, &raw); err != nil {
		return nil, err
	}
	return &User{
		ID:            raw.ID,
		Name:          raw.Name,
		WorkspaceName: raw.Bot.WorkspaceName,
		MaxFileUpload: raw.Bot.WorkspaceLimits.MaxFileUpload,
		BotType:       raw.Type,
	}, nil
}

// SearchParentPages finds shared databases and pages that can serve as the
// archive location. Notion's search index only contains objects explicitly
// shared with the integration and can lag after sharing changes. Data sources
// are returned so the UI can prefer them, and inline database parent pages are
// also expanded so page-based creation remains available.
func (c *Client) SearchParentPages(ctx context.Context, query string) (*ParentPageSearch, error) {
	var results []searchResult
	cursor := ""
	// Search can split a result set across pages. Ten pages cover up to 1,000
	// shared objects; the user can also narrow the search or paste a link.
	for page := 0; page < 10; page++ {
		body := map[string]any{"query": query, "page_size": 100}
		if cursor != "" {
			body["start_cursor"] = cursor
		}
		var out struct {
			Results    []searchResult `json:"results"`
			HasMore    bool           `json:"has_more"`
			NextCursor string         `json:"next_cursor"`
		}
		if err := c.do(ctx, http.MethodPost, "/search", body, &out); err != nil {
			return nil, err
		}
		results = append(results, out.Results...)
		if !out.HasMore || out.NextCursor == "" {
			break
		}
		cursor = out.NextCursor
	}

	found := &ParentPageSearch{
		Pages:       make([]Page, 0, len(results)),
		DataSources: make([]DataSource, 0, len(results)),
	}
	seen := map[string]bool{}
	seenSources := map[string]bool{}
	parentPageIDs := make([]string, 0, 4)
	for _, result := range results {
		switch result.Object {
		case "page":
			if result.InTrash || result.ID == "" || seen[result.ID] {
				continue
			}
			// Rows inside a database are pages too, but a database cannot be
			// created under them meaningfully.
			if result.Parent.Type == "data_source_id" {
				continue
			}
			page := Page{
				ID: result.ID, CreatedTime: result.CreatedTime,
				LastEditedTime: result.LastEditedTime, InTrash: result.InTrash,
				Archived: result.Archived, URL: result.URL, Parent: result.Parent,
				Properties: map[string]PropertyValue{},
			}
			if len(result.Properties) > 0 {
				if err := json.Unmarshal(result.Properties, &page.Properties); err != nil {
					continue
				}
			}
			if notionTitle := TitleFromPage(page); notionTitle == "" && len(result.Title) > 0 {
				page.Properties[TitlePropertyName] = PropertyValue{Type: "title", Title: result.Title}
			}
			seen[result.ID] = true
			found.Pages = append(found.Pages, page)
		case "data_source":
			if result.InTrash || result.ID == "" {
				continue
			}
			if !seenSources[result.ID] {
				seenSources[result.ID] = true
				title := ""
				for _, t := range result.Title {
					title += t.PlainText
				}
				if title == "" {
					title = "未命名数据库"
				}
				found.DataSources = append(found.DataSources, DataSource{
					ID:         result.ID,
					DatabaseID: result.Parent.DatabaseID,
					URL:        result.URL,
					Title:      title,
				})
			}
			if result.DatabaseParent.Type == "page_id" && result.DatabaseParent.PageID != "" {
				parentPageIDs = append(parentPageIDs, result.DatabaseParent.PageID)
			}
		}
	}

	sort.Slice(found.DataSources, func(i, j int) bool {
		return strings.ToLower(found.DataSources[i].Title) < strings.ToLower(found.DataSources[j].Title)
	})

	// An inline database lives on a page. If the integration was shared the
	// database, its page may still be retrievable even though search does not
	// list it. Cap the extra lookups so a workspace with many shared databases
	// does not turn one search into dozens of rate-limited requests.
	tried := map[string]bool{}
	for _, id := range parentPageIDs {
		if seen[id] || tried[id] || len(tried) >= 10 {
			continue
		}
		tried[id] = true
		page, err := c.GetPage(ctx, id)
		if err != nil || page.InTrash {
			continue
		}
		seen[id] = true
		found.Pages = append(found.Pages, *page)
	}
	return found, nil
}

// searchResult is one entry from POST /search. A data source's properties are
// a schema, not page property values, so they are decoded only for pages.
type searchResult struct {
	Object         string          `json:"object"`
	ID             string          `json:"id"`
	CreatedTime    time.Time       `json:"created_time"`
	LastEditedTime time.Time       `json:"last_edited_time"`
	InTrash        bool            `json:"in_trash"`
	Archived       bool            `json:"archived"`
	URL            string          `json:"url"`
	Title          []RichText      `json:"title"`
	Parent         Parent          `json:"parent"`
	Properties     json.RawMessage `json:"properties"`
	DatabaseParent Parent          `json:"database_parent"`
}

// SearchTrash lists manually trashed pages. Indexing lags; best-effort only.
func (c *Client) SearchTrash(ctx context.Context) ([]Page, error) {
	body := map[string]any{
		"page_size": 100,
		"filter":    map[string]any{"property": "object", "value": "page", "in_trash": true},
	}
	var out struct {
		Results []Page `json:"results"`
	}
	if err := c.do(ctx, http.MethodPost, "/search", body, &out); err != nil {
		return nil, err
	}
	return out.Results, nil
}

// CreateMemoDatabase creates the app-owned database and returns its data source
// with every property applied.
func (c *Client) CreateMemoDatabase(ctx context.Context, parentPageID, title string, properties map[string]PropertySchema) (*DataSource, error) {
	body := map[string]any{
		"parent":     map[string]any{"type": "page_id", "page_id": parentPageID},
		"title":      []RichText{{Type: "text", Text: &TextContent{Content: title}}},
		"properties": properties,
	}
	var created struct {
		ID          string `json:"id"`
		DataSources []struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"data_sources"`
		Properties map[string]PropertySchema `json:"properties"`
	}
	if err := c.do(ctx, http.MethodPost, "/databases", body, &created); err != nil {
		return nil, err
	}
	dataSourceID := ""
	if len(created.DataSources) > 0 {
		dataSourceID = created.DataSources[0].ID
	}
	if dataSourceID == "" {
		ds, err := c.retrieveDatabaseDataSource(ctx, created.ID)
		if err != nil {
			return nil, err
		}
		dataSourceID = ds
	}
	if err := c.UpdateDataSourceSchema(ctx, dataSourceID, withoutTitle(properties)); err != nil {
		return nil, err
	}
	ds, err := c.RetrieveDataSource(ctx, dataSourceID)
	if err != nil {
		return nil, err
	}
	ds.DatabaseID = created.ID
	return ds, nil
}

func (c *Client) retrieveDatabaseDataSource(ctx context.Context, databaseID string) (string, error) {
	var db struct {
		DataSources []struct {
			ID string `json:"id"`
		} `json:"data_sources"`
	}
	if err := c.do(ctx, http.MethodGet, "/databases/"+databaseID, nil, &db); err != nil {
		return "", err
	}
	if len(db.DataSources) == 0 {
		return "", fmt.Errorf("notion: database %s has no data source", databaseID)
	}
	return db.DataSources[0].ID, nil
}

// ResolveDataSource accepts a data source ID or a database ID and returns the
// data source that owns the schema. Notion URLs usually carry the database ID,
// while search results carry the data source ID.
func (c *Client) ResolveDataSource(ctx context.Context, id string) (*DataSource, error) {
	ds, err := c.RetrieveDataSource(ctx, id)
	if err == nil {
		return ds, nil
	}
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.Status != 404 {
		return nil, err
	}
	dataSourceID, lookupErr := c.retrieveDatabaseDataSource(ctx, id)
	if lookupErr != nil {
		return nil, err
	}
	return c.RetrieveDataSource(ctx, dataSourceID)
}

// RetrieveDataSource reads the table schema and property types.
func (c *Client) RetrieveDataSource(ctx context.Context, dataSourceID string) (*DataSource, error) {
	var ds DataSource
	if err := c.do(ctx, http.MethodGet, "/data_sources/"+dataSourceID, nil, &ds); err != nil {
		return nil, err
	}
	if ds.PropertyTypes == nil {
		ds.PropertyTypes = map[string]string{}
	}
	return &ds, nil
}

// UpdateDataSourceSchema adds or changes properties.
func (c *Client) UpdateDataSourceSchema(ctx context.Context, dataSourceID string, properties map[string]PropertySchema) error {
	if len(properties) == 0 {
		return nil
	}
	return c.do(ctx, http.MethodPatch, "/data_sources/"+dataSourceID, map[string]any{"properties": properties}, nil)
}

// QueryDataSource runs one data-source query page.
func (c *Client) QueryDataSource(ctx context.Context, dataSourceID string, req QueryRequest) (*QueryResponse, error) {
	var out QueryResponse
	if err := c.do(ctx, http.MethodPost, "/data_sources/"+dataSourceID+"/query", req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// CreatePage creates one memo page with up to 100 children.
func (c *Client) CreatePage(ctx context.Context, dataSourceID string, properties map[string]PropertyValue, children []Block) (*Page, error) {
	body := map[string]any{
		"parent":     map[string]any{"type": "data_source_id", "data_source_id": dataSourceID},
		"properties": properties,
	}
	if len(children) > 0 {
		body["children"] = children[:min(len(children), 100)]
	}
	var page Page
	if err := c.do(ctx, http.MethodPost, "/pages", body, &page); err != nil {
		return nil, err
	}
	return &page, nil
}

// UpdatePage patches properties without touching the body.
func (c *Client) UpdatePage(ctx context.Context, pageID string, properties map[string]PropertyValue) error {
	return c.do(ctx, http.MethodPatch, "/pages/"+pageID, map[string]any{"properties": properties}, nil)
}

// GetPage retrieves one page, including trashed pages.
func (c *Client) GetPage(ctx context.Context, pageID string) (*Page, error) {
	var page Page
	if err := c.do(ctx, http.MethodGet, "/pages/"+pageID, nil, &page); err != nil {
		return nil, err
	}
	return &page, nil
}

// ListChildren returns one page of a block's children.
func (c *Client) ListChildren(ctx context.Context, blockID, cursor string, pageSize int) (*ChildrenResponse, error) {
	if pageSize <= 0 || pageSize > 100 {
		pageSize = 100
	}
	q := url.Values{}
	q.Set("page_size", strconv.Itoa(pageSize))
	if cursor != "" {
		q.Set("start_cursor", cursor)
	}
	var out ChildrenResponse
	if err := c.do(ctx, http.MethodGet, "/blocks/"+blockID+"/children?"+q.Encode(), nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// AppendChildren appends up to 100 blocks at the end.
func (c *Client) AppendChildren(ctx context.Context, blockID string, children []Block) (*ChildrenResponse, error) {
	body := map[string]any{
		"children": children,
		"position": map[string]any{"type": "end"},
	}
	var out ChildrenResponse
	if err := c.do(ctx, http.MethodPatch, "/blocks/"+blockID+"/children", body, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// DeleteBlock removes one block (used by body replacement).
func (c *Client) DeleteBlock(ctx context.Context, blockID string) error {
	return c.do(ctx, http.MethodDelete, "/blocks/"+blockID, nil, nil)
}

// UploadFile creates a single-part file upload and sends the bytes.
func (c *Client) UploadFile(ctx context.Context, filename, contentType string, data []byte) (string, error) {
	var created struct {
		ID        string `json:"id"`
		UploadURL string `json:"upload_url"`
	}
	body := map[string]any{"filename": filename, "content_type": contentType}
	if err := c.do(ctx, http.MethodPost, "/file_uploads", body, &created); err != nil {
		return "", err
	}
	sendURL := created.UploadURL
	if sendURL == "" {
		sendURL = c.baseURL + "/file_uploads/" + created.ID + "/send"
	}
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	part, err := mw.CreateFormFile("file", filename)
	if err != nil {
		return "", err
	}
	if _, err := part.Write(data); err != nil {
		return "", err
	}
	if err := mw.Close(); err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, sendURL, &buf)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Notion-Version", APIVersion)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	if err := c.limiter.wait(ctx); err != nil {
		return "", err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", parseAPIError(resp)
	}
	return created.ID, nil
}

// Download fetches a signed Notion file URL (no bearer token). The URL comes
// from an API response but is treated as untrusted: HTTPS only, and never a
// loopback/private address.
func (c *Client) Download(ctx context.Context, rawURL string) ([]byte, error) {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return nil, err
	}
	if err := validateRemoteURL(parsed); err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("notion: download %d", resp.StatusCode)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 64<<20))
}

func newSafeHTTPClient() *http.Client {
	return &http.Client{
		Timeout: 45 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 5 {
				return fmt.Errorf("notion: too many redirects")
			}
			return validateRemoteURL(req.URL)
		},
	}
}

func validateRemoteURL(u *url.URL) error {
	if u == nil || u.Scheme != "https" {
		return fmt.Errorf("notion: file download must use https")
	}
	host := strings.ToLower(u.Hostname())
	if host == "" || host == "localhost" || strings.HasSuffix(host, ".local") || strings.HasSuffix(host, ".internal") {
		return fmt.Errorf("notion: refusing to download from %q", host)
	}
	if ip := net.ParseIP(host); ip != nil {
		if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsUnspecified() {
			return fmt.Errorf("notion: refusing to download from private address %q", host)
		}
	}
	return nil
}

func (c *Client) do(ctx context.Context, method, path string, body any, out any) error {
	var payload []byte
	if body != nil {
		var err error
		payload, err = json.Marshal(body)
		if err != nil {
			return err
		}
	}
	var lastErr error
	for attempt := 0; attempt <= c.maxRetries; attempt++ {
		if err := c.limiter.wait(ctx); err != nil {
			return err
		}
		req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, bytes.NewReader(payload))
		if err != nil {
			return err
		}
		req.Header.Set("Authorization", "Bearer "+c.token)
		req.Header.Set("Notion-Version", APIVersion)
		if body != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		resp, err := c.http.Do(req)
		if err != nil {
			lastErr = err
			continue
		}
		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			defer resp.Body.Close()
			if out == nil {
				_, _ = io.Copy(io.Discard, resp.Body)
				return nil
			}
			return json.NewDecoder(resp.Body).Decode(out)
		}
		apiErr := parseAPIError(resp)
		resp.Body.Close()
		lastErr = apiErr
		if apiErr.IsRateLimited() && attempt < c.maxRetries {
			delay := apiErr.RetryAfter
			if delay <= 0 {
				delay = time.Second
			}
			if delay > 60*time.Second {
				delay = 60 * time.Second
			}
			timer := time.NewTimer(delay)
			select {
			case <-ctx.Done():
				timer.Stop()
				return ctx.Err()
			case <-timer.C:
			}
			continue
		}
		return apiErr
	}
	return lastErr
}

func parseAPIError(resp *http.Response) *APIError {
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	var raw struct {
		Status     int    `json:"status"`
		Code       string `json:"code"`
		Message    string `json:"message"`
		Additional struct {
			RetryAfter          string   `json:"retry_after"`
			CommittedResourceID string   `json:"committed_resource_id"`
			CommittedChildIDs   []string `json:"committed_child_ids"`
		} `json:"additional_data"`
	}
	_ = json.Unmarshal(data, &raw)
	e := &APIError{Status: resp.StatusCode, Code: raw.Code, Message: raw.Message}
	if e.Status == 0 {
		e.Status = resp.StatusCode
	}
	if raw.Additional.RetryAfter != "" {
		if secs, err := strconv.Atoi(raw.Additional.RetryAfter); err == nil {
			e.RetryAfter = time.Duration(secs) * time.Second
		}
	}
	if e.RetryAfter == 0 {
		if secs, err := strconv.Atoi(resp.Header.Get("Retry-After")); err == nil {
			e.RetryAfter = time.Duration(secs) * time.Second
		}
	}
	e.CommittedResourceID = raw.Additional.CommittedResourceID
	e.CommittedChildIDs = raw.Additional.CommittedChildIDs
	return e
}

// withoutTitle removes the title property before a schema PATCH; the title is
// created with the database and must not be redefined.
func withoutTitle(properties map[string]PropertySchema) map[string]PropertySchema {
	out := make(map[string]PropertySchema, len(properties))
	for name, schema := range properties {
		if schema.Title != nil {
			continue
		}
		out[name] = schema
	}
	return out
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
