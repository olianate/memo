package notion

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func testClient(t *testing.T, handler http.HandlerFunc) *Client {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return NewClient("secret-token", WithBaseURL(srv.URL), WithRateInterval(0))
}

func TestMeSendsPinnedHeaders(t *testing.T) {
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/users/me" {
			t.Errorf("request = %s %s", r.Method, r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer secret-token" {
			t.Errorf("authorization = %q", got)
		}
		if got := r.Header.Get("Notion-Version"); got != APIVersion {
			t.Errorf("version = %q", got)
		}
		_, _ = w.Write([]byte(`{"id":"u1","name":"bot","type":"bot","bot":{"workspace_name":"Mine","workspace_limits":{"max_file_upload_size_in_bytes":5242880}}}`))
	})
	me, err := c.Me(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if me.WorkspaceName != "Mine" || me.MaxFileUpload != 5242880 {
		t.Fatalf("me = %+v", me)
	}
}

func TestParsePageID(t *testing.T) {
	cases := map[string]string{
		"0123456789abcdef0123456789abcdef":                                     "0123456789abcdef0123456789abcdef",
		"01234567-89ab-cdef-0123-456789abcdef":                                 "0123456789abcdef0123456789abcdef",
		"https://www.notion.so/My-Page-0123456789abcdef0123456789abcdef?pvs=4": "0123456789abcdef0123456789abcdef",
		"https://app.notion.com/p/01234567-89ab-cdef-0123-456789abcdef":        "0123456789abcdef0123456789abcdef",
		"https://example.com/no-id":                                            "",
		"":                                                                     "",
	}
	for input, want := range cases {
		if got := ParsePageID(input); got != want {
			t.Errorf("ParsePageID(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestSearchParentPagesExpandsDataSourcePage(t *testing.T) {
	requests := 0
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/search":
			requests++
			_, _ = w.Write([]byte(`{"results":[
				{"object":"page","id":"page-1","url":"https://notion.so/page-1","properties":{"名称":{"type":"title","title":[{"plain_text":"Archive"}]}}},
				{"object":"data_source","id":"ds-1","url":"https://notion.so/db-1","title":[{"plain_text":"memo probe"}],"parent":{"type":"database_id","database_id":"db-1"},"database_parent":{"type":"page_id","page_id":"page-2"}}
			],"has_more":false}`))
		case r.Method == http.MethodGet && r.URL.Path == "/pages/page-2":
			_, _ = w.Write([]byte(`{"object":"page","id":"page-2","url":"https://notion.so/page-2","properties":{"名称":{"type":"title","title":[{"plain_text":"Inline page"}]}}}`))
		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
	})
	found, err := c.SearchParentPages(context.Background(), "memo")
	if err != nil {
		t.Fatal(err)
	}
	if requests != 1 {
		t.Fatalf("search requests = %d", requests)
	}
	if len(found.Pages) != 2 {
		t.Fatalf("pages = %+v", found.Pages)
	}
	if len(found.DataSources) != 1 {
		t.Fatalf("data sources = %+v", found.DataSources)
	}
	ds := found.DataSources[0]
	if ds.ID != "ds-1" || ds.DatabaseID != "db-1" || ds.Title != "memo probe" || ds.URL != "https://notion.so/db-1" {
		t.Fatalf("data source = %+v", ds)
	}
}

func TestResolveDataSourceFallsBackToDatabaseID(t *testing.T) {
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/data_sources/db-1":
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"status":404,"code":"object_not_found","message":"not a data source"}`))
		case "/databases/db-1":
			_, _ = w.Write([]byte(`{"id":"db-1","data_sources":[{"id":"ds-1"}]}`))
		case "/data_sources/ds-1":
			_, _ = w.Write([]byte(`{"id":"ds-1","url":"https://notion.so/db-1","title":[{"plain_text":"memo"}],"parent":{"database_id":"db-1"},"properties":{"名称":{"type":"title"}}}`))
		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
	})
	ds, err := c.ResolveDataSource(context.Background(), "db-1")
	if err != nil {
		t.Fatal(err)
	}
	if ds.ID != "ds-1" || ds.DatabaseID != "db-1" || ds.TitleProperty() != "名称" {
		t.Fatalf("data source = %+v", ds)
	}
}

func TestMissingSchema(t *testing.T) {
	existing := map[string]string{"名称": "title", PropTags: "multi_select"}
	missing, err := MissingSchema(existing, AppSchema("名称"))
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := missing["名称"]; ok {
		t.Fatal("title property should not be patched")
	}
	if _, ok := missing[PropTags]; ok {
		t.Fatal("existing property should not be patched")
	}
	if _, ok := missing[PropMemoID]; !ok {
		t.Fatal("missing Memo ID property not returned")
	}
	existing[PropTags] = "rich_text"
	if _, err := MissingSchema(existing, AppSchema("名称")); err == nil {
		t.Fatal("type conflict was not reported")
	}
}

func TestSearchTrashBody(t *testing.T) {
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		filter := body["filter"].(map[string]any)
		if filter["in_trash"] != true {
			t.Errorf("filter = %+v", filter)
		}
		_, _ = w.Write([]byte(`{"results":[{"id":"p1","in_trash":true,"properties":{}}]}`))
	})
	pages, err := c.SearchTrash(context.Background())
	if err != nil || len(pages) != 1 {
		t.Fatalf("pages = %+v, %v", pages, err)
	}
}

func TestQueryDataSourceReportsIncomplete(t *testing.T) {
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/data_sources/ds1/query" {
			t.Errorf("path = %s", r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"results":[],"has_more":false,"request_status":{"type":"incomplete","incomplete_reason":"query_result_limit_reached"}}`))
	})
	resp, err := c.QueryDataSource(context.Background(), "ds1", QueryRequest{PageSize: 100})
	if err != nil {
		t.Fatal(err)
	}
	if !resp.Incomplete() {
		t.Fatal("expected incomplete query")
	}
}

func TestRateLimitRetryHonoursRetryAfter(t *testing.T) {
	calls := 0
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls == 1 {
			w.Header().Set("Retry-After", "0")
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte(`{"status":429,"code":"rate_limited","message":"slow down"}`))
			return
		}
		_, _ = w.Write([]byte(`{"id":"u1"}`))
	})
	if _, err := c.Me(context.Background()); err != nil {
		t.Fatal(err)
	}
	if calls != 2 {
		t.Fatalf("calls = %d", calls)
	}
}

func Test503CommittedResourceID(t *testing.T) {
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte(`{"status":503,"code":"service_unavailable","message":"retry","additional_data":{"committed_resource_id":"page-123","committed_child_ids":["b1"]}}`))
	})
	_, err := c.CreatePage(context.Background(), "ds1", nil, nil)
	apiErr, ok := err.(*APIError)
	if !ok {
		t.Fatalf("err = %T %v", err, err)
	}
	if !apiErr.CommitLanded() || apiErr.CommittedResourceID != "page-123" || len(apiErr.CommittedChildIDs) != 1 {
		t.Fatalf("api error = %+v", apiErr)
	}
}

func TestCreateMemoDatabaseDiscoversTitle(t *testing.T) {
	var created bool
	var patched bool
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/databases":
			created = true
			_, _ = w.Write([]byte(`{"id":"db1","data_sources":[{"id":"ds1","name":"memo"}]}`))
		case r.Method == http.MethodPatch && r.URL.Path == "/data_sources/ds1":
			patched = true
			_, _ = w.Write([]byte(`{}`))
		case r.Method == http.MethodGet && r.URL.Path == "/data_sources/ds1":
			_, _ = w.Write([]byte(`{"id":"ds1","parent":{"type":"database_id","database_id":"db1"},"title":[{"type":"text","text":{"content":"memo"}}],"properties":{"名称":{"id":"title","type":"title"},"Memo ID":{"id":"m","type":"rich_text"}}}`))
		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
	})
	ds, err := c.CreateMemoDatabase(context.Background(), "parent1", "memo", AppSchema("名称"))
	if err != nil {
		t.Fatal(err)
	}
	if !created || !patched {
		t.Fatalf("created=%v patched=%v", created, patched)
	}
	if ds.ID != "ds1" || ds.DatabaseID != "db1" || ds.TitleProperty() != "名称" {
		t.Fatalf("data source = %+v", ds)
	}
}

func TestUploadFileFlow(t *testing.T) {
	var gotFile bool
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/file_uploads":
			_, _ = w.Write([]byte(`{"id":"up1","upload_url":"` + "http://" + r.Host + `/upload/up1"}`))
		case "/upload/up1":
			if err := r.ParseMultipartForm(10 << 20); err != nil {
				t.Errorf("multipart: %v", err)
			}
			if _, _, err := r.FormFile("file"); err == nil {
				gotFile = true
			}
			_, _ = w.Write([]byte(`{"id":"up1","status":"uploaded"}`))
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
		}
	})
	id, err := c.UploadFile(context.Background(), "a.png", "image/png", []byte("bytes"))
	if err != nil {
		t.Fatal(err)
	}
	if id != "up1" || !gotFile {
		t.Fatalf("id=%q gotFile=%v", id, gotFile)
	}
}

func TestDownloadRejectsUnsafeURLs(t *testing.T) {
	c := NewClient("tok", WithBaseURL("http://127.0.0.1"), WithRateInterval(0))
	for _, raw := range []string{"http://example.com/a", "https://127.0.0.1/a", "https://localhost/a"} {
		if _, err := c.Download(context.Background(), raw); err == nil {
			t.Errorf("Download accepted %q", raw)
		}
	}
}

func TestListChildrenPaginationQuery(t *testing.T) {
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("page_size") != "50" || r.URL.Query().Get("start_cursor") != "cur" {
			t.Errorf("query = %s", r.URL.RawQuery)
		}
		_, _ = w.Write([]byte(`{"results":[],"has_more":true,"next_cursor":"next"}`))
	})
	resp, err := c.ListChildren(context.Background(), "page1", "cur", 50)
	if err != nil || !resp.HasMore || resp.NextCursor != "next" {
		t.Fatalf("resp = %+v %v", resp, err)
	}
}
