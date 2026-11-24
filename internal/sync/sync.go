// Package sync implements the durable outbox push, Notion polling pull,
// conflict resolution, local eviction, hydration, and restore-all.
package sync

import (
	"context"
	"errors"
	"math/rand"
	"sync"
	"time"

	"github.com/olianate/memo/internal/config"
	"github.com/olianate/memo/internal/domain"
	"github.com/olianate/memo/internal/notion"
	"github.com/olianate/memo/internal/presence"
	"github.com/olianate/memo/internal/store"
)

// SetPresence wires the body-presence module. Retention calls it to evict, so
// the eviction order lives in one module.
func (s *Service) SetPresence(p *presence.Keeper) { s.presence = p }

// Secrets is the keychain port. The token is never persisted in SQLite.
type Secrets interface {
	NotionToken(ctx context.Context) (string, error)
	SetNotionToken(ctx context.Context, token string) error
	ClearNotionToken(ctx context.Context) error
}

// AttachmentImporter writes attachment bytes fetched from Notion and deletes
// local files during eviction. The attachment service implements it.
type AttachmentImporter interface {
	SaveRemote(ctx context.Context, memoID, filename, mime string, kind domain.AttachmentKind, data []byte) (*domain.Attachment, error)
	DeleteFiles(ctx context.Context, a domain.Attachment) error
}

// Status is the UI-facing sync state.
type Status struct {
	State         domain.SyncStatus `json:"state"`
	Enabled       bool              `json:"enabled"`
	Paused        bool              `json:"paused"`
	Configured    bool              `json:"configured"`
	Active        bool              `json:"active"`
	LastSuccess   *time.Time        `json:"last_success,omitempty"`
	Pending       int               `json:"pending"`
	Retrying      int               `json:"retrying"`
	LastError     string            `json:"last_error,omitempty"`
	WorkspaceName string            `json:"workspace_name,omitempty"`
	DataSourceID  string            `json:"data_source_id,omitempty"`
	Logs          []store.SyncLog   `json:"logs"`
}

// Service owns the sync workers. One instance per app.
type Service struct {
	store    *store.Store
	cfg      *config.Store
	secrets  Secrets
	importer AttachmentImporter
	presence *presence.Keeper

	clock          func() time.Time
	rand           *rand.Rand
	gatewayFactory func(token string) notion.Gateway

	opMu          sync.Mutex
	mu            sync.Mutex
	gateway       notion.Gateway // test injection
	deviceID      string
	syncing       bool
	lastError     string
	active        bool
	started       bool
	lastRetention time.Time
	stopCh        chan struct{}
	wakeCh        chan struct{}
	onEvent       func(name string, data any)
}

// New builds a sync service.
func New(st *store.Store, cfg *config.Store, secrets Secrets, importer AttachmentImporter) *Service {
	return &Service{
		store:    st,
		cfg:      cfg,
		secrets:  secrets,
		importer: importer,
		clock:    time.Now,
		rand:     rand.New(rand.NewSource(time.Now().UnixNano())),
		gatewayFactory: func(token string) notion.Gateway {
			return notion.NewClient(token)
		},
		stopCh: make(chan struct{}),
		wakeCh: make(chan struct{}, 1),
	}
}

// SetGateway injects a fake gateway (tests) or a prebuilt client.
func (s *Service) SetGateway(g notion.Gateway) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.gateway = g
}

// SetDeviceID sets the writer identity before workers start.
func (s *Service) SetDeviceID(id string) { s.deviceID = id }

// SetClock injects a clock for deterministic tests.
func (s *Service) SetClock(fn func() time.Time) { s.clock = fn }

// OnEvent registers a Wails event emitter. Events are emitted outside locks.
func (s *Service) OnEvent(fn func(name string, data any)) { s.onEvent = fn }

// SetActive marks whether a window is active (60 s poll vs 5 min idle).
func (s *Service) SetActive(active bool) {
	s.mu.Lock()
	s.active = active
	s.mu.Unlock()
	if active {
		s.Wake()
	}
}

// Wake nudges the worker to run now.
func (s *Service) Wake() {
	select {
	case s.wakeCh <- struct{}{}:
	default:
	}
}

// Start launches the background worker. Safe to call once.
func (s *Service) Start(ctx context.Context) {
	s.mu.Lock()
	if s.started {
		s.mu.Unlock()
		return
	}
	s.started = true
	s.mu.Unlock()
	go s.loop(ctx)
}

// Stop terminates the worker.
func (s *Service) Stop() {
	s.mu.Lock()
	if !s.started {
		s.mu.Unlock()
		return
	}
	s.started = false
	close(s.stopCh)
	s.mu.Unlock()
}

func (s *Service) loop(ctx context.Context) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-s.stopCh:
			return
		case <-ticker.C:
			s.tick(ctx)
		case <-s.wakeCh:
			s.tick(ctx)
		}
	}
}

func (s *Service) tick(ctx context.Context) {
	if !s.canSync(ctx) {
		return
	}
	s.opMu.Lock()
	defer s.opMu.Unlock()
	if _, err := s.PushOnce(ctx); err != nil {
		s.recordError(err)
	}
	state, _ := s.store.DeviceState(ctx)
	now := s.clock()

	pullInterval := 5 * time.Minute
	if s.isActive() {
		pullInterval = time.Minute
	}
	if state.LastPullAt == nil || now.Sub(*state.LastPullAt) >= pullInterval {
		if err := s.PullOnce(ctx); err != nil {
			s.recordError(err)
		}
	}
	if state.LastFullScanAt == nil || now.Sub(*state.LastFullScanAt) >= 24*time.Hour {
		if err := s.FullScan(ctx); err != nil {
			s.recordError(err)
		}
	}
	if state.LastTrashSweep == nil || now.Sub(*state.LastTrashSweep) >= 24*time.Hour {
		if err := s.TrashSweep(ctx); err != nil {
			s.recordError(err)
		}
	}
	if s.lastRetention.IsZero() || now.Sub(s.lastRetention) >= time.Hour {
		if err := s.EvictOnce(ctx); err != nil {
			s.recordError(err)
		} else {
			s.lastRetention = now
		}
	}
}

// SyncNow forces a push and an incremental pull.
func (s *Service) SyncNow(ctx context.Context) error {
	if !s.canSync(ctx) {
		return errors.New("sync is off or not connected")
	}
	s.opMu.Lock()
	defer s.opMu.Unlock()
	if _, err := s.PushOnce(ctx); err != nil {
		return err
	}
	if err := s.PullOnce(ctx); err != nil {
		return err
	}
	s.recordSuccess()
	return nil
}

// Status reports the current state for the UI.
func (s *Service) Status(ctx context.Context) Status {
	st := Status{
		Enabled:       s.cfg.Bool(ctx, config.KeySyncEnabled),
		Paused:        s.cfg.Bool(ctx, config.KeySyncPaused),
		Configured:    s.cfg.Bool(ctx, config.KeyNotionConfigured) && s.cfg.Get(ctx, config.KeyNotionDataSourceID) != "",
		WorkspaceName: s.cfg.Get(ctx, config.KeyNotionWorkspace),
		DataSourceID:  s.cfg.Get(ctx, config.KeyNotionDataSourceID),
		Logs:          []store.SyncLog{},
	}
	pending, retrying, err := s.store.OutboxCounts(ctx)
	if err == nil {
		st.Pending = pending
		st.Retrying = retrying
	}
	if logs, err := s.store.Logs(ctx, 20); err == nil {
		st.Logs = logs
	}
	if v := s.cfg.Get(ctx, config.KeyLastSyncAt); v != "" {
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			st.LastSuccess = &t
		}
	}
	s.mu.Lock()
	st.Active = s.active
	st.LastError = s.lastError
	syncing := s.syncing
	s.mu.Unlock()
	switch {
	case !st.Enabled:
		st.State = domain.SyncIdle
	case st.Paused:
		st.State = domain.SyncPaused
	case st.LastError != "":
		st.State = domain.SyncError
	case syncing:
		st.State = domain.SyncSyncing
	default:
		st.State = domain.SyncIdle
	}
	return st
}

// SetPaused pauses or resumes workers without changing the connection.
func (s *Service) SetPaused(ctx context.Context, paused bool) error {
	if err := s.cfg.SetBool(ctx, config.KeySyncPaused, paused); err != nil {
		return err
	}
	s.emit("sync:status", s.Status(ctx))
	if !paused {
		s.Wake()
	}
	return nil
}

// Enable turns global sync on and queues every local-only memo.
func (s *Service) Enable(ctx context.Context, enabled bool) error {
	if err := s.cfg.SetBool(ctx, config.KeySyncEnabled, enabled); err != nil {
		return err
	}
	if enabled {
		if err := s.store.EnqueueAllLocalOnly(ctx); err != nil {
			return err
		}
		s.Wake()
	}
	s.emit("sync:status", s.Status(ctx))
	return nil
}

func (s *Service) canSync(ctx context.Context) bool {
	if !s.cfg.Bool(ctx, config.KeySyncEnabled) || s.cfg.Bool(ctx, config.KeySyncPaused) {
		return false
	}
	return s.cfg.Get(ctx, config.KeyNotionDataSourceID) != ""
}

func (s *Service) isActive() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.active
}

func (s *Service) gatewayFor(ctx context.Context) (notion.Gateway, string, error) {
	s.mu.Lock()
	g := s.gateway
	s.mu.Unlock()
	ds := s.cfg.Get(ctx, config.KeyNotionDataSourceID)
	if ds == "" {
		return nil, "", errors.New("notion is not connected")
	}
	if g != nil {
		return g, ds, nil
	}
	token, err := s.secrets.NotionToken(ctx)
	if err != nil || token == "" {
		return nil, "", errors.New("notion token is missing from the keychain")
	}
	g = s.gatewayFactory(token)
	return g, ds, nil
}

func (s *Service) recordError(err error) {
	if err == nil {
		return
	}
	s.mu.Lock()
	s.lastError = err.Error()
	s.syncing = false
	s.mu.Unlock()
	ctx := context.Background()
	_ = s.store.Log(ctx, "error", "sync", err.Error())
	s.emit("sync:status", s.Status(ctx))
}

func (s *Service) recordSuccess() {
	now := s.clock()
	ctx := context.Background()
	s.mu.Lock()
	s.lastError = ""
	s.syncing = false
	s.mu.Unlock()
	_ = s.cfg.Set(ctx, config.KeyLastSyncAt, now.UTC().Format(time.RFC3339))
	s.emit("sync:status", s.Status(ctx))
}

// setBusy records whether a push or pull is in flight and notifies the UI on
// transitions. Without the notification the last sampled "syncing" state would
// stick in the status banner after a pull that pushed nothing.
func (s *Service) setBusy(busy bool) {
	s.mu.Lock()
	changed := s.syncing != busy
	s.syncing = busy
	s.mu.Unlock()
	if changed {
		s.emit("sync:status", s.Status(context.Background()))
	}
}

func (s *Service) emit(name string, data any) {
	if s.onEvent != nil {
		s.onEvent(name, data)
	}
}

// Config exposes the settings store to callers that need the keys.
func (s *Service) Config() *config.Store { return s.cfg }

// Store exposes the local store (services only; never the frontend).
func (s *Service) Store() *store.Store { return s.store }

func (s *Service) device(ctx context.Context) string {
	if s.deviceID != "" {
		return s.deviceID
	}
	id := s.cfg.Get(ctx, config.KeyDeviceID)
	if id == "" {
		id = domain.NewID()
		_ = s.cfg.Set(ctx, config.KeyDeviceID, id)
	}
	s.deviceID = id
	return id
}

func backoffDelay(attempts int, rnd *rand.Rand) time.Duration {
	if attempts < 1 {
		attempts = 1
	}
	base := time.Second << min(attempts-1, 8) // 1s .. ~4m
	if base > 5*time.Minute {
		base = 5 * time.Minute
	}
	jitter := 0.0
	if rnd != nil {
		jitter = rnd.Float64()
	}
	return base + time.Duration(float64(base)*0.25*jitter)
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func isAPIError(err error, predicate func(*notion.APIError) bool) bool {
	var apiErr *notion.APIError
	return errors.As(err, &apiErr) && predicate(apiErr)
}
