package services

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/olianate/memo/internal/config"
	"github.com/olianate/memo/internal/notion"
	"github.com/olianate/memo/internal/store"
	syncengine "github.com/olianate/memo/internal/sync"
)

// WorkspaceDTO is the validated Notion workspace summary.
type WorkspaceDTO struct {
	Name      string `json:"name"`
	ID        string `json:"id"`
	FileLimit int64  `json:"file_limit"`
	BotType   string `json:"bot_type"`
}

// ParentPageDTO is a Notion page the user can choose when no database is
// shared yet; memo will create its own database under it.
type ParentPageDTO struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	URL   string `json:"url"`
}

// ArchiveDatabaseDTO is a database already shared with the integration. ID is
// the data source ID used by sync; DatabaseID is kept for display and for
// resolving links.
type ArchiveDatabaseDTO struct {
	ID         string `json:"id"`
	DatabaseID string `json:"database_id"`
	Title      string `json:"title"`
	URL        string `json:"url"`
}

// ArchiveTargetDTO is a resolved archive location, either a shared database or
// a page that can contain a newly created database.
type ArchiveTargetDTO struct {
	Kind  string `json:"kind"` // "database" or "page"
	ID    string `json:"id"`
	Title string `json:"title"`
	URL   string `json:"url"`
}

// ParentPageList is the archive-location search result. Databases are the
// preferred target; pages are the fallback when no database is shared.
type ParentPageList struct {
	Databases []ArchiveDatabaseDTO `json:"databases"`
	Pages     []ParentPageDTO      `json:"pages"`
}

// SchemaPropertyDTO is one property a connection would add to an existing
// database.
type SchemaPropertyDTO struct {
	Name string `json:"name"`
	Type string `json:"type"`
}

// SchemaConflictDTO is a same-named property with an incompatible type.
type SchemaConflictDTO struct {
	Name         string `json:"name"`
	ExistingType string `json:"existing_type"`
	RequiredType string `json:"required_type"`
}

// ArchiveDatabasePreview reports what connecting a shared database would
// change. Only additive schema changes are allowed.
type ArchiveDatabasePreview struct {
	ID                string              `json:"id"`
	Title             string              `json:"title"`
	TitleProperty     string              `json:"title_property"`
	MissingProperties []SchemaPropertyDTO `json:"missing_properties"`
	Conflicts         []SchemaConflictDTO `json:"conflicts"`
	HasExistingRows   bool                `json:"has_existing_rows"`
}

// ConnectRequest selects an existing shared database or a page to create one
// under. DatabaseName is only used for the page path, and
// ConfirmSchemaChanges must be true before memo adds missing properties to an
// existing database.
type ConnectRequest struct {
	Token                string `json:"token"`
	Kind                 string `json:"kind"`
	TargetID             string `json:"target_id"`
	DatabaseName         string `json:"database_name"`
	ConfirmSchemaChanges bool   `json:"confirm_schema_changes"`
}

// ConnectResult reports the archive that was connected or created.
type ConnectResult struct {
	Workspace       WorkspaceDTO `json:"workspace"`
	DataSourceID    string       `json:"data_source_id"`
	DatabaseID      string       `json:"database_id"`
	DatabaseTitle   string       `json:"database_title"`
	TitleProperty   string       `json:"title_property"`
	CreatedDatabase bool         `json:"created_database"`
}

// SyncService is the onboarding and sync-control boundary.
type SyncService struct {
	engine  *syncengine.Service
	cfg     *config.Store
	store   *store.Store
	secrets syncengine.Secrets
	client  func(token string) notion.Gateway
	emit    func(string, any)
}

// NewSyncService builds the sync boundary.
func NewSyncService(engine *syncengine.Service, cfg *config.Store, st *store.Store, secrets syncengine.Secrets, emit func(string, any)) *SyncService {
	return &SyncService{
		engine: engine, cfg: cfg, store: st, secrets: secrets, emit: emit,
		client: func(token string) notion.Gateway { return notion.NewClient(token) },
	}
}

// Status returns the sync state machine plus recent log lines.
func (s *SyncService) Status() syncengine.Status {
	return s.engine.Status(context.Background())
}

// SetEnabled turns global sync on or off. Enabling queues every local-only memo.
func (s *SyncService) SetEnabled(enabled bool) error {
	if enabled && !s.cfg.Bool(context.Background(), config.KeyNotionConfigured) {
		return errors.New("请先连接 Notion")
	}
	if err := s.engine.Enable(context.Background(), enabled); err != nil {
		return err
	}
	s.emitStatus()
	return nil
}

// SetPaused pauses or resumes the workers without disconnecting.
func (s *SyncService) SetPaused(paused bool) error {
	if err := s.engine.SetPaused(context.Background(), paused); err != nil {
		return err
	}
	s.emitStatus()
	return nil
}

// SyncNow forces an immediate push and pull.
func (s *SyncService) SyncNow() error {
	err := s.engine.SyncNow(context.Background())
	s.emitStatus()
	return err
}

// SetActive tells the poller whether a window is in the foreground.
func (s *SyncService) SetActive(active bool) { s.engine.SetActive(active) }

// ValidateToken checks a personal access token with users.me.
func (s *SyncService) ValidateToken(token string) (info WorkspaceDTO, err error) {
	defer s.logFailure("notion.validate_token", &err)
	token = strings.TrimSpace(token)
	if token == "" {
		return WorkspaceDTO{}, errors.New("请输入 Notion 集成令牌")
	}
	me, err := s.client(token).Me(context.Background())
	if err != nil {
		return WorkspaceDTO{}, friendlyNotionError(err)
	}
	return WorkspaceDTO{Name: me.WorkspaceName, ID: me.ID, FileLimit: me.MaxFileUpload, BotType: me.BotType}, nil
}

// ParentPages lists shared databases and pages (the integration must have
// access). Databases are the preferred target; pages are for creating one.
func (s *SyncService) ParentPages(token, query string) (out ParentPageList, err error) {
	defer s.logFailure("notion.parent_pages", &err)
	token = strings.TrimSpace(token)
	if token == "" {
		return ParentPageList{}, errors.New("请输入 Notion 集成令牌")
	}
	found, err := s.client(token).SearchParentPages(context.Background(), strings.TrimSpace(query))
	if err != nil {
		return ParentPageList{}, friendlyNotionError(err)
	}
	out = ParentPageList{
		Databases: make([]ArchiveDatabaseDTO, 0, len(found.DataSources)),
		Pages:     make([]ParentPageDTO, 0, len(found.Pages)),
	}
	for _, ds := range found.DataSources {
		title := strings.TrimSpace(ds.Title)
		if title == "" {
			title = "未命名数据库"
		}
		out.Databases = append(out.Databases, ArchiveDatabaseDTO{
			ID:         ds.ID,
			DatabaseID: ds.DatabaseID,
			Title:      title,
			URL:        ds.URL,
		})
	}
	for _, p := range found.Pages {
		dto, err := parentPageDTO(p)
		if err != nil {
			continue
		}
		out.Pages = append(out.Pages, dto)
	}
	return out, nil
}

// ResolveArchiveTarget validates a pasted database/page link or ID. Search can
// lag behind sharing changes, so this is the escape hatch that works
// immediately. Databases are preferred when the link matches both kinds.
func (s *SyncService) ResolveArchiveTarget(token, target string) (out ArchiveTargetDTO, err error) {
	defer s.logFailure("notion.resolve_target", &err)
	token = strings.TrimSpace(token)
	if token == "" {
		return ArchiveTargetDTO{}, errors.New("请输入 Notion 集成令牌")
	}
	id := notion.ParsePageID(target)
	if id == "" {
		return ArchiveTargetDTO{}, errors.New("无法识别 Notion 链接或 ID。请在 Notion 菜单中复制链接后重试")
	}
	ctx := context.Background()
	client := s.client(token)
	pageObj, pageErr := client.GetPage(ctx, id)
	if pageErr == nil {
		if pageObj.InTrash {
			return ArchiveTargetDTO{}, errors.New("这个页面在 Notion 回收站里，请先恢复它")
		}
		dto, err := parentPageDTO(*pageObj)
		if err != nil {
			return ArchiveTargetDTO{}, err
		}
		return ArchiveTargetDTO{Kind: "page", ID: dto.ID, Title: dto.Title, URL: dto.URL}, nil
	}
	ds, err := client.ResolveDataSource(ctx, id)
	if err != nil {
		return ArchiveTargetDTO{}, friendlyNotionError(pageErr)
	}
	return archiveDatabaseTarget(ds), nil
}

func archiveDatabaseTarget(ds *notion.DataSource) ArchiveTargetDTO {
	title := strings.TrimSpace(ds.Title)
	if title == "" {
		title = "未命名数据库"
	}
	return ArchiveTargetDTO{Kind: "database", ID: ds.ID, Title: title, URL: ds.URL}
}

// PreviewArchiveDatabase checks what connecting an existing database would
// change. It never writes anything.
func (s *SyncService) PreviewArchiveDatabase(token, target string) (preview ArchiveDatabasePreview, err error) {
	defer s.logFailure("notion.preview_database", &err)
	token = strings.TrimSpace(token)
	if token == "" {
		return ArchiveDatabasePreview{}, errors.New("请输入 Notion 集成令牌")
	}
	id := notion.ParsePageID(target)
	if id == "" {
		return ArchiveDatabasePreview{}, errors.New("无法识别数据库链接或 ID")
	}
	ctx := context.Background()
	client := s.client(token)
	ds, err := client.ResolveDataSource(ctx, id)
	if err != nil {
		return ArchiveDatabasePreview{}, friendlyNotionError(err)
	}
	titleProp := ds.TitleProperty()
	if titleProp == "" {
		return ArchiveDatabasePreview{}, errors.New("这个数据库没有标题属性，无法作为 memo 归档")
	}
	plan := notion.PlanSchema(ds.PropertyTypes, notion.AppSchema(titleProp))
	title := strings.TrimSpace(ds.Title)
	if title == "" {
		title = "未命名数据库"
	}
	preview = ArchiveDatabasePreview{
		ID:                ds.ID,
		Title:             title,
		TitleProperty:     titleProp,
		MissingProperties: make([]SchemaPropertyDTO, 0, len(plan.Missing)),
		Conflicts:         make([]SchemaConflictDTO, 0, len(plan.Conflicts)),
	}
	names := make([]string, 0, len(plan.Missing))
	for name := range plan.Missing {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		preview.MissingProperties = append(preview.MissingProperties, SchemaPropertyDTO{
			Name: name,
			Type: notion.PropertyTypeName(plan.Missing[name]),
		})
	}
	for _, conflict := range plan.Conflicts {
		preview.Conflicts = append(preview.Conflicts, SchemaConflictDTO{
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

func parentPageDTO(p notion.Page) (ParentPageDTO, error) {
	if p.Parent.Type == "data_source_id" {
		return ParentPageDTO{}, errors.New("这是数据库中的记录，不能作为父页面。请选择一个普通页面")
	}
	title := notion.TitleFromPage(p)
	if title == "" {
		title = "未命名页面"
	}
	return ParentPageDTO{ID: p.ID, Title: title, URL: p.URL}, nil
}

// Connect validates the token, connects an existing shared database or creates
// the app-owned database under a page, stores the token in the OS keychain, and
// turns sync on.
func (s *SyncService) Connect(req ConnectRequest) (result ConnectResult, err error) {
	defer s.logFailure("notion.connect", &err)
	token := strings.TrimSpace(req.Token)
	targetID := notion.ParsePageID(req.TargetID)
	if token == "" || strings.TrimSpace(req.TargetID) == "" {
		return ConnectResult{}, errors.New("令牌和归档位置都必填")
	}
	if targetID == "" {
		return ConnectResult{}, errors.New("无法识别 Notion 链接或 ID。请在 Notion 菜单中复制链接后重试")
	}
	kind := strings.ToLower(strings.TrimSpace(req.Kind))
	if kind != "database" && kind != "page" {
		return ConnectResult{}, errors.New("请选择共享的数据库，或选择用于新建数据库的页面")
	}
	ctx := context.Background()
	if s.cfg.Bool(ctx, config.KeyNotionConfigured) {
		return ConnectResult{}, errors.New("已经连接了一个 Notion 归档，请先断开再重新连接")
	}
	client := s.client(token)
	me, err := client.Me(ctx)
	if err != nil {
		return ConnectResult{}, friendlyNotionError(err)
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
			return ConnectResult{}, friendlyNotionError(err)
		}
		titleProp = ds.TitleProperty()
		if titleProp == "" {
			return ConnectResult{}, errors.New("这个数据库没有标题属性，无法作为 memo 归档")
		}
		plan := notion.PlanSchema(ds.PropertyTypes, notion.AppSchema(titleProp))
		if len(plan.Conflicts) > 0 {
			c := plan.Conflicts[0]
			return ConnectResult{}, fmt.Errorf("数据库中的属性「%s」类型是 %s，memo 需要 %s；请改名或调整该属性", c.Name, c.ExistingType, c.RequiredType)
		}
		if len(plan.Missing) > 0 {
			if !req.ConfirmSchemaChanges {
				return ConnectResult{}, errors.New("该数据库需要新增 memo 属性，请先查看并确认 schema 变更后再连接")
			}
			if err := client.UpdateDataSourceSchema(ctx, ds.ID, plan.Missing); err != nil {
				return ConnectResult{}, friendlyNotionError(err)
			}
			ds, err = client.RetrieveDataSource(ctx, ds.ID)
			if err != nil {
				return ConnectResult{}, friendlyNotionError(err)
			}
			titleProp = ds.TitleProperty()
		}
	case "page":
		parent, err := client.GetPage(ctx, targetID)
		if err != nil {
			return ConnectResult{}, friendlyNotionError(err)
		}
		if parent.InTrash {
			return ConnectResult{}, errors.New("这个页面在 Notion 回收站里，请先恢复它")
		}
		if _, err := parentPageDTO(*parent); err != nil {
			return ConnectResult{}, err
		}
		name := strings.TrimSpace(req.DatabaseName)
		if name == "" {
			name = "memo"
		}
		ds, err = client.CreateMemoDatabase(ctx, targetID, name, notion.AppSchema(notion.TitlePropertyName))
		if err != nil {
			return ConnectResult{}, friendlyNotionError(err)
		}
		titleProp = ds.TitleProperty()
		if titleProp == "" {
			titleProp = notion.TitlePropertyName
		}
		parentPageID = targetID
		createdDatabase = true
	}

	if err := s.secrets.SetNotionToken(ctx, token); err != nil {
		return ConnectResult{}, fmt.Errorf("无法写入系统钥匙串: %w", err)
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
		if err := s.cfg.Set(ctx, k, v); err != nil {
			return ConnectResult{}, err
		}
	}
	if err := s.store.EnqueueAllLocalOnly(ctx); err != nil {
		return ConnectResult{}, err
	}
	s.engine.Wake()
	s.emitStatus()
	databaseTitle := strings.TrimSpace(ds.Title)
	if databaseTitle == "" {
		databaseTitle = "未命名数据库"
	}
	if createdDatabase {
		s.log("info", "notion.connect", fmt.Sprintf("已在页面下创建并连接数据库「%s」", databaseTitle))
	} else {
		s.log("info", "notion.connect", fmt.Sprintf("已连接共享数据库「%s」", databaseTitle))
	}
	return ConnectResult{
		Workspace:       WorkspaceDTO{Name: me.WorkspaceName, ID: me.ID, FileLimit: limit, BotType: me.BotType},
		DataSourceID:    ds.ID,
		DatabaseID:      ds.DatabaseID,
		DatabaseTitle:   databaseTitle,
		TitleProperty:   titleProp,
		CreatedDatabase: createdDatabase,
	}, nil
}

// Disconnect clears the token and connection state but keeps local memos.
func (s *SyncService) Disconnect() error {
	ctx := context.Background()
	var firstErr error
	note := func(err error) {
		if err != nil && firstErr == nil {
			firstErr = err
		}
	}
	note(s.secrets.ClearNotionToken(ctx))
	for _, key := range []string{
		config.KeyNotionDataSourceID, config.KeyNotionDatabaseID, config.KeyNotionParentPageID,
		config.KeyNotionTitleProp, config.KeyNotionWorkspace,
	} {
		note(s.cfg.Set(ctx, key, ""))
	}
	note(s.cfg.SetBool(ctx, config.KeyNotionConfigured, false))
	note(s.cfg.SetBool(ctx, config.KeySyncEnabled, false))
	note(s.cfg.SetBool(ctx, config.KeySyncPaused, false))
	s.emitStatus()
	if firstErr != nil {
		s.log("error", "notion.disconnect", firstErr.Error())
	} else {
		s.log("info", "notion.disconnect", "已断开 Notion 归档")
	}
	return firstErr
}

// RestoreAll hydrates every evicted memo with progress events.
func (s *SyncService) RestoreAll() (syncengine.RestoreProgress, error) {
	return s.engine.RestoreAll(context.Background())
}

// Hydrate fetches one evicted memo from Notion.
func (s *SyncService) Hydrate(memoID string) error {
	return s.engine.Hydrate(context.Background(), memoID)
}

// Logs returns the bounded sync log.
func (s *SyncService) Logs() ([]store.SyncLog, error) {
	return s.store.Logs(context.Background(), 50)
}

func (s *SyncService) emitStatus() {
	if s.emit != nil {
		s.emit("sync:status", s.engine.Status(context.Background()))
	}
}

func friendlyNotionError(err error) error {
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

// PlatformService exposes harmless window intents to the frontend.
type PlatformService struct {
	hooks PlatformHooks
	cfg   *config.Store
}

// PlatformHooks are implemented in main with the Wails application.
type PlatformHooks interface {
	ShowMainWindow()
	ShowSettings()
	TakePendingView() string
	ShowStickyWindow()
	HideStickyWindow()
	Quit()
	SaveStickyPosition(x, y int) error
	StickyPosition() (int, int)
}

// NewPlatformService builds the platform boundary.
func NewPlatformService(hooks PlatformHooks, cfg *config.Store) *PlatformService {
	return &PlatformService{hooks: hooks, cfg: cfg}
}

// ShowMain brings the main window forward.
func (s *PlatformService) ShowMain() { s.hooks.ShowMainWindow() }

// ShowSettings brings the main window forward and opens settings.
func (s *PlatformService) ShowSettings() { s.hooks.ShowSettings() }

// TakePendingView returns and clears a navigation request that may have been
// created before the frontend was ready.
func (s *PlatformService) TakePendingView() string { return s.hooks.TakePendingView() }

// ShowSticky summons quick capture.
func (s *PlatformService) ShowSticky() { s.hooks.ShowStickyWindow() }

// HideSticky hides quick capture without losing the draft.
func (s *PlatformService) HideSticky() { s.hooks.HideStickyWindow() }

// Quit exits the app.
func (s *PlatformService) Quit() { s.hooks.Quit() }

// SaveStickyPosition persists the sticky window position.
func (s *PlatformService) SaveStickyPosition(x, y int) error {
	return s.hooks.SaveStickyPosition(x, y)
}

// StickyPosition returns the remembered sticky position.
func (s *PlatformService) StickyPosition() (int, int) { return s.hooks.StickyPosition() }

// CurrentTheme is read by the frontend before the first paint.
func (s *PlatformService) CurrentTheme() string {
	return s.cfg.Get(context.Background(), config.KeyTheme)
}
