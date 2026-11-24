package services

import (
	"context"
	"errors"

	"github.com/olianate/memo/internal/archive"
	"github.com/olianate/memo/internal/config"
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

// SyncService is the sync-control and onboarding boundary. Onboarding rules
// live in the archive module; these methods are thin adapters over it.
type SyncService struct {
	engine *syncengine.Service
	store  *store.Store
	link   *archive.Connector
	emit   func(string, any)
}

// NewSyncService builds the sync boundary.
func NewSyncService(engine *syncengine.Service, cfg *config.Store, st *store.Store, secrets syncengine.Secrets, emit func(string, any)) *SyncService {
	s := &SyncService{engine: engine, store: st, emit: emit}
	s.link = archive.New(cfg, secrets, st, nil, engine.Wake, s.emitStatus, s.log)
	return s
}

// Status returns the sync state machine plus recent log lines.
func (s *SyncService) Status() syncengine.Status {
	return s.engine.Status(context.Background())
}

// SetEnabled turns global sync on or off. Enabling queues every local-only memo.
// The engine emits its own status transition.
func (s *SyncService) SetEnabled(enabled bool) error {
	ctx := context.Background()
	if enabled && !s.link.Connected(ctx) {
		return errors.New("请先连接 Notion")
	}
	return s.engine.Enable(ctx, enabled)
}

// SetPaused pauses or resumes the workers without disconnecting.
func (s *SyncService) SetPaused(paused bool) error {
	return s.engine.SetPaused(context.Background(), paused)
}

// SyncNow forces an immediate push and pull.
func (s *SyncService) SyncNow() error { return s.engine.SyncNow(context.Background()) }

// SetActive tells the poller whether a window is in the foreground.
func (s *SyncService) SetActive(active bool) { s.engine.SetActive(active) }

// ValidateToken checks a personal access token with users.me.
func (s *SyncService) ValidateToken(token string) (WorkspaceDTO, error) {
	info, err := s.link.ValidateToken(context.Background(), token)
	if err != nil {
		return WorkspaceDTO{}, err
	}
	return toWorkspaceDTO(info), nil
}

// ParentPages lists shared databases and pages (the integration must have
// access). Databases are the preferred target; pages are for creating one.
func (s *SyncService) ParentPages(token, query string) (ParentPageList, error) {
	found, err := s.link.ParentPages(context.Background(), token, query)
	if err != nil {
		return ParentPageList{}, err
	}
	out := ParentPageList{
		Databases: make([]ArchiveDatabaseDTO, 0, len(found.Databases)),
		Pages:     make([]ParentPageDTO, 0, len(found.Pages)),
	}
	for _, ds := range found.Databases {
		out.Databases = append(out.Databases, ArchiveDatabaseDTO{
			ID:         ds.ID,
			DatabaseID: ds.DatabaseID,
			Title:      ds.Title,
			URL:        ds.URL,
		})
	}
	for _, p := range found.Pages {
		out.Pages = append(out.Pages, ParentPageDTO{ID: p.ID, Title: p.Title, URL: p.URL})
	}
	return out, nil
}

// ResolveArchiveTarget validates a pasted database/page link or ID.
func (s *SyncService) ResolveArchiveTarget(token, target string) (ArchiveTargetDTO, error) {
	found, err := s.link.ResolveTarget(context.Background(), token, target)
	if err != nil {
		return ArchiveTargetDTO{}, err
	}
	return ArchiveTargetDTO{Kind: found.Kind, ID: found.ID, Title: found.Title, URL: found.URL}, nil
}

// PreviewArchiveDatabase checks what connecting an existing database would
// change. It never writes anything.
func (s *SyncService) PreviewArchiveDatabase(token, target string) (ArchiveDatabasePreview, error) {
	preview, err := s.link.PreviewDatabase(context.Background(), token, target)
	if err != nil {
		return ArchiveDatabasePreview{}, err
	}
	out := ArchiveDatabasePreview{
		ID:                preview.ID,
		Title:             preview.Title,
		TitleProperty:     preview.TitleProperty,
		MissingProperties: make([]SchemaPropertyDTO, 0, len(preview.MissingProperties)),
		Conflicts:         make([]SchemaConflictDTO, 0, len(preview.Conflicts)),
		HasExistingRows:   preview.HasExistingRows,
	}
	for _, property := range preview.MissingProperties {
		out.MissingProperties = append(out.MissingProperties, SchemaPropertyDTO{Name: property.Name, Type: property.Type})
	}
	for _, conflict := range preview.Conflicts {
		out.Conflicts = append(out.Conflicts, SchemaConflictDTO{
			Name:         conflict.Name,
			ExistingType: conflict.ExistingType,
			RequiredType: conflict.RequiredType,
		})
	}
	return out, nil
}

// Connect validates the token, connects an existing shared database or creates
// the app-owned database under a page, stores the token in the OS keychain, and
// turns sync on.
func (s *SyncService) Connect(req ConnectRequest) (ConnectResult, error) {
	result, err := s.link.Connect(context.Background(), archive.ConnectRequest{
		Token:                req.Token,
		Kind:                 req.Kind,
		TargetID:             req.TargetID,
		DatabaseName:         req.DatabaseName,
		ConfirmSchemaChanges: req.ConfirmSchemaChanges,
	})
	if err != nil {
		return ConnectResult{}, err
	}
	return ConnectResult{
		Workspace:       toWorkspaceDTO(result.Workspace),
		DataSourceID:    result.DataSourceID,
		DatabaseID:      result.DatabaseID,
		DatabaseTitle:   result.DatabaseTitle,
		TitleProperty:   result.TitleProperty,
		CreatedDatabase: result.CreatedDatabase,
	}, nil
}

// Disconnect clears the token and connection state but keeps local memos.
func (s *SyncService) Disconnect() error {
	return s.link.Disconnect(context.Background())
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

func toWorkspaceDTO(info archive.Workspace) WorkspaceDTO {
	return WorkspaceDTO{Name: info.Name, ID: info.ID, FileLimit: info.FileLimit, BotType: info.BotType}
}
