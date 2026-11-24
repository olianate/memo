// Package archive owns the Notion archive connection: validating a token,
// finding a target, planning schema changes, and persisting the connection.
//
// The Wails boundary and the onboarding screen are adapters over this module;
// the rules about what may be connected live here.
package archive

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/olianate/memo/internal/config"
	"github.com/olianate/memo/internal/notion"
)

// Workspace is the validated Notion workspace summary.
type Workspace struct {
	Name      string
	ID        string
	FileLimit int64
	BotType   string
}

// ParentPage is a Notion page the user can choose when no database is shared
// yet; memo will create its own database under it.
type ParentPage struct {
	ID    string
	Title string
	URL   string
}

// Database is a database already shared with the integration. ID is the data
// source ID used by sync; DatabaseID is kept for display and for resolving
// links.
type Database struct {
	ID         string
	DatabaseID string
	Title      string
	URL        string
}

// Target is a resolved archive location, either a shared database or a page
// that can contain a newly created database.
type Target struct {
	Kind  string // "database" or "page"
	ID    string
	Title string
	URL   string
}

// PageList is the archive-location search result. Databases are the preferred
// target; pages are the fallback when no database is shared.
type PageList struct {
	Databases []Database
	Pages     []ParentPage
}

// SchemaProperty is one property a connection would add to an existing
// database.
type SchemaProperty struct {
	Name string
	Type string
}

// SchemaConflict is a same-named property with an incompatible type.
type SchemaConflict struct {
	Name         string
	ExistingType string
	RequiredType string
}

// Preview reports what connecting a shared database would change. Only
// additive schema changes are allowed.
type Preview struct {
	ID                string
	Title             string
	TitleProperty     string
	MissingProperties []SchemaProperty
	Conflicts         []SchemaConflict
	HasExistingRows   bool
}

// ConnectRequest selects an existing shared database or a page to create one
// under. DatabaseName is only used for the page path, and
// ConfirmSchemaChanges must be true before memo adds missing properties to an
// existing database.
type ConnectRequest struct {
	Token                string
	Kind                 string
	TargetID             string
	DatabaseName         string
	ConfirmSchemaChanges bool
}

// Result reports the archive that was connected or created.
type Result struct {
	Workspace       Workspace
	DataSourceID    string
	DatabaseID      string
	DatabaseTitle   string
	TitleProperty   string
	CreatedDatabase bool
}

// Secrets is the keychain port.
type Secrets interface {
	SetNotionToken(ctx context.Context, token string) error
	ClearNotionToken(ctx context.Context) error
}

// Outbox is the store port needed on connect.
type Outbox interface {
	EnqueueAllLocalOnly(ctx context.Context) error
}

// Logger records a bounded diagnostic event.
type Logger func(level, event, message string)

// Connector is the Notion archive connection module.
type Connector struct {
	cfg     *config.Store
	secrets Secrets
	outbox  Outbox
	client  func(token string) notion.Gateway
	wake    func()
	changed func()
	log     Logger
}

// New builds the connector. wake and changed may be nil; log may be nil.
func New(cfg *config.Store, secrets Secrets, outbox Outbox, client func(token string) notion.Gateway, wake, changed func(), log Logger) *Connector {
	if client == nil {
		client = func(token string) notion.Gateway { return notion.NewClient(token) }
	}
	if wake == nil {
		wake = func() {}
	}
	if changed == nil {
		changed = func() {}
	}
	if log == nil {
		log = func(string, string, string) {}
	}
	return &Connector{cfg: cfg, secrets: secrets, outbox: outbox, client: client, wake: wake, changed: changed, log: log}
}

// ValidateToken checks a personal access token with users.me.
func (c *Connector) ValidateToken(ctx context.Context, token string) (info Workspace, err error) {
	defer c.logFailure("notion.validate_token", &err)
	token = strings.TrimSpace(token)
	if token == "" {
		return Workspace{}, errors.New("请输入 Notion 集成令牌")
	}
	me, err := c.client(token).Me(ctx)
	if err != nil {
		return Workspace{}, friendlyError(err)
	}
	return Workspace{Name: me.WorkspaceName, ID: me.ID, FileLimit: me.MaxFileUpload, BotType: me.BotType}, nil
}

// ParentPages lists shared databases and pages. Databases are the preferred
// target; pages are for creating one.
func (c *Connector) ParentPages(ctx context.Context, token, query string) (out PageList, err error) {
	defer c.logFailure("notion.parent_pages", &err)
	token = strings.TrimSpace(token)
	if token == "" {
		return PageList{}, errors.New("请输入 Notion 集成令牌")
	}
	found, err := c.client(token).SearchParentPages(ctx, strings.TrimSpace(query))
	if err != nil {
		return PageList{}, friendlyError(err)
	}
	out = PageList{
		Databases: make([]Database, 0, len(found.DataSources)),
		Pages:     make([]ParentPage, 0, len(found.Pages)),
	}
	for _, ds := range found.DataSources {
		out.Databases = append(out.Databases, Database{
			ID:         ds.ID,
			DatabaseID: ds.DatabaseID,
			Title:      databaseTitle(ds.Title),
			URL:        ds.URL,
		})
	}
	for _, p := range found.Pages {
		dto, err := parentPage(p)
		if err != nil {
			continue
		}
		out.Pages = append(out.Pages, dto)
	}
	return out, nil
}

// ResolveTarget validates a pasted database/page link or ID. Search can lag
// behind sharing changes; databases win when the link matches both kinds.
func (c *Connector) ResolveTarget(ctx context.Context, token, target string) (out Target, err error) {
	defer c.logFailure("notion.resolve_target", &err)
	token = strings.TrimSpace(token)
	if token == "" {
		return Target{}, errors.New("请输入 Notion 集成令牌")
	}
	id := notion.ParsePageID(target)
	if id == "" {
		return Target{}, errors.New("无法识别 Notion 链接或 ID。请在 Notion 菜单中复制链接后重试")
	}
	client := c.client(token)
	page, pageErr := client.GetPage(ctx, id)
	if pageErr == nil {
		if page.InTrash {
			return Target{}, errors.New("这个页面在 Notion 回收站里，请先恢复它")
		}
		dto, err := parentPage(*page)
		if err != nil {
			return Target{}, err
		}
		return Target{Kind: "page", ID: dto.ID, Title: dto.Title, URL: dto.URL}, nil
	}
	ds, err := client.ResolveDataSource(ctx, id)
	if err != nil {
		return Target{}, friendlyError(pageErr)
	}
	return databaseTarget(ds), nil
}

// PreviewDatabase checks what connecting an existing database would change. It
// never writes anything.
func (c *Connector) PreviewDatabase(ctx context.Context, token, target string) (preview Preview, err error) {
	defer c.logFailure("notion.preview_database", &err)
	token = strings.TrimSpace(token)
	if token == "" {
		return Preview{}, errors.New("请输入 Notion 集成令牌")
	}
	id := notion.ParsePageID(target)
	if id == "" {
		return Preview{}, errors.New("无法识别数据库链接或 ID")
	}
	client := c.client(token)
	ds, err := client.ResolveDataSource(ctx, id)
	if err != nil {
		return Preview{}, friendlyError(err)
	}
	titleProp := ds.TitleProperty()
	if titleProp == "" {
		return Preview{}, errors.New("这个数据库没有标题属性，无法作为 memo 归档")
	}
	plan := notion.PlanSchema(ds.PropertyTypes, notion.AppSchema(titleProp))
	preview = Preview{
		ID:                ds.ID,
		Title:             databaseTitle(ds.Title),
		TitleProperty:     titleProp,
		MissingProperties: make([]SchemaProperty, 0, len(plan.Missing)),
		Conflicts:         make([]SchemaConflict, 0, len(plan.Conflicts)),
	}
	names := make([]string, 0, len(plan.Missing))
	for name := range plan.Missing {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		preview.MissingProperties = append(preview.MissingProperties, SchemaProperty{
			Name: name,
			Type: notion.PropertyTypeName(plan.Missing[name]),
		})
	}
	for _, conflict := range plan.Conflicts {
		preview.Conflicts = append(preview.Conflicts, SchemaConflict{
			Name:         conflict.Name,
			ExistingType: conflict.ExistingType,
			RequiredType: conflict.RequiredType,
		})
	}
	if resp, err := client.QueryDataSource(ctx, ds.ID, notion.QueryRequest{PageSize: 1}); err == nil {
		preview.HasExistingRows = len(resp.Results) > 0
	}
	return preview, nil
}

// Connect validates the token, connects an existing shared database or creates
// the app-owned database under a page, stores the token in the OS keychain,
// queues existing memos, and turns sync on.
func (c *Connector) Connect(ctx context.Context, req ConnectRequest) (result Result, err error) {
	defer c.logFailure("notion.connect", &err)
	token := strings.TrimSpace(req.Token)
	targetID := notion.ParsePageID(req.TargetID)
	if token == "" || strings.TrimSpace(req.TargetID) == "" {
		return Result{}, errors.New("令牌和归档位置都必填")
	}
	if targetID == "" {
		return Result{}, errors.New("无法识别 Notion 链接或 ID。请在 Notion 菜单中复制链接后重试")
	}
	kind := strings.ToLower(strings.TrimSpace(req.Kind))
	if kind != "database" && kind != "page" {
		return Result{}, errors.New("请选择共享的数据库，或选择用于新建数据库的页面")
	}
	if c.cfg.Bool(ctx, config.KeyNotionConfigured) {
		return Result{}, errors.New("已经连接了一个 Notion 归档，请先断开再重新连接")
	}
	client := c.client(token)
	me, err := client.Me(ctx)
	if err != nil {
		return Result{}, friendlyError(err)
	}

	var (
		ds              *notion.DataSource
		parentPageID    string
		createdDatabase bool
	)
	titleProp := ""
	switch kind {
	case "database":
		ds, err = client.ResolveDataSource(ctx, targetID)
		if err != nil {
			return Result{}, friendlyError(err)
		}
		titleProp = ds.TitleProperty()
		if titleProp == "" {
			return Result{}, errors.New("这个数据库没有标题属性，无法作为 memo 归档")
		}
		plan := notion.PlanSchema(ds.PropertyTypes, notion.AppSchema(titleProp))
		if len(plan.Conflicts) > 0 {
			first := plan.Conflicts[0]
			return Result{}, fmt.Errorf("数据库中的属性「%s」类型是 %s，memo 需要 %s；请改名或调整该属性", first.Name, first.ExistingType, first.RequiredType)
		}
		if len(plan.Missing) > 0 {
			if !req.ConfirmSchemaChanges {
				return Result{}, errors.New("该数据库需要新增 memo 属性，请先查看并确认 schema 变更后再连接")
			}
			if err := client.UpdateDataSourceSchema(ctx, ds.ID, plan.Missing); err != nil {
				return Result{}, friendlyError(err)
			}
			ds, err = client.RetrieveDataSource(ctx, ds.ID)
			if err != nil {
				return Result{}, friendlyError(err)
			}
			titleProp = ds.TitleProperty()
		}
	case "page":
		parent, err := client.GetPage(ctx, targetID)
		if err != nil {
			return Result{}, friendlyError(err)
		}
		if parent.InTrash {
			return Result{}, errors.New("这个页面在 Notion 回收站里，请先恢复它")
		}
		if _, err := parentPage(*parent); err != nil {
			return Result{}, err
		}
		name := strings.TrimSpace(req.DatabaseName)
		if name == "" {
			name = "memo"
		}
		ds, err = client.CreateMemoDatabase(ctx, targetID, name, notion.AppSchema(notion.TitlePropertyName))
		if err != nil {
			return Result{}, friendlyError(err)
		}
		titleProp = ds.TitleProperty()
		if titleProp == "" {
			titleProp = notion.TitlePropertyName
		}
		parentPageID = targetID
		createdDatabase = true
	}

	if err := c.secrets.SetNotionToken(ctx, token); err != nil {
		return Result{}, fmt.Errorf("无法写入系统钥匙串: %w", err)
	}
	limit := me.MaxFileUpload
	if limit <= 0 {
		limit = 5 << 20
	}
	settings := map[string]string{
		config.KeyNotionConfigured:   "true",
		config.KeyNotionDataSourceID: ds.ID,
		config.KeyNotionDatabaseID:   ds.DatabaseID,
		config.KeyNotionParentPageID: parentPageID,
		config.KeyNotionTitleProp:    titleProp,
		config.KeyNotionWorkspace:    me.WorkspaceName,
		config.KeyWorkspaceFileLimit: fmt.Sprintf("%d", limit),
		config.KeySyncEnabled:        "true",
		config.KeySyncPaused:         "false",
	}
	for k, v := range settings {
		if err := c.cfg.Set(ctx, k, v); err != nil {
			return Result{}, err
		}
	}
	if err := c.outbox.EnqueueAllLocalOnly(ctx); err != nil {
		return Result{}, err
	}
	c.wake()
	c.changed()
	databaseTitle := databaseTitle(ds.Title)
	if createdDatabase {
		c.log("info", "notion.connect", fmt.Sprintf("已在页面下创建并连接数据库「%s」", databaseTitle))
	} else {
		c.log("info", "notion.connect", fmt.Sprintf("已连接共享数据库「%s」", databaseTitle))
	}
	return Result{
		Workspace:       Workspace{Name: me.WorkspaceName, ID: me.ID, FileLimit: limit, BotType: me.BotType},
		DataSourceID:    ds.ID,
		DatabaseID:      ds.DatabaseID,
		DatabaseTitle:   databaseTitle,
		TitleProperty:   titleProp,
		CreatedDatabase: createdDatabase,
	}, nil
}

// Connected reports whether an archive has been set up already.
func (c *Connector) Connected(ctx context.Context) bool {
	return c.cfg.Bool(ctx, config.KeyNotionConfigured)
}

// Disconnect clears the token and connection state but keeps local memos.
func (c *Connector) Disconnect(ctx context.Context) error {
	var firstErr error
	note := func(err error) {
		if err != nil && firstErr == nil {
			firstErr = err
		}
	}
	note(c.secrets.ClearNotionToken(ctx))
	for _, key := range []string{
		config.KeyNotionDataSourceID, config.KeyNotionDatabaseID, config.KeyNotionParentPageID,
		config.KeyNotionTitleProp, config.KeyNotionWorkspace,
	} {
		note(c.cfg.Set(ctx, key, ""))
	}
	note(c.cfg.SetBool(ctx, config.KeyNotionConfigured, false))
	note(c.cfg.SetBool(ctx, config.KeySyncEnabled, false))
	note(c.cfg.SetBool(ctx, config.KeySyncPaused, false))
	c.changed()
	if firstErr != nil {
		c.log("error", "notion.disconnect", firstErr.Error())
	} else {
		c.log("info", "notion.disconnect", "已断开 Notion 归档")
	}
	return firstErr
}

func (c *Connector) logFailure(event string, err *error) {
	if err == nil || *err == nil {
		return
	}
	c.log("error", event, (*err).Error())
}

func parentPage(p notion.Page) (ParentPage, error) {
	if p.Parent.Type == "data_source_id" {
		return ParentPage{}, errors.New("这是数据库中的记录，不能作为父页面。请选择一个普通页面")
	}
	title := notion.TitleFromPage(p)
	if title == "" {
		title = "未命名页面"
	}
	return ParentPage{ID: p.ID, Title: title, URL: p.URL}, nil
}

func databaseTarget(ds *notion.DataSource) Target {
	return Target{Kind: "database", ID: ds.ID, Title: databaseTitle(ds.Title), URL: ds.URL}
}

func databaseTitle(title string) string {
	title = strings.TrimSpace(title)
	if title == "" {
		return "未命名数据库"
	}
	return title
}

// friendlyError turns common Notion API failures into actionable messages.
func friendlyError(err error) error {
	var apiErr *notion.APIError
	if errors.As(err, &apiErr) {
		switch {
		case apiErr.Status == 401:
			return errors.New("令牌无效：请在 Notion 中重新生成集成令牌")
		case apiErr.Status == 403:
			return errors.New("集成没有权限：请在 Notion 页面中把集成添加为连接")
		case apiErr.Status == 404:
			return errors.New("找不到页面或数据库：请确认集成已被添加到目标页面，或链接是否正确")
		case apiErr.Status == 429 || apiErr.Status == 529:
			return errors.New("Notion 正在限流，请稍后再试")
		case apiErr.Status >= 500:
			return errors.New("Notion 暂时不可用，请稍后再试")
		}
	}
	return err
}
