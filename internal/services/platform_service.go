package services

import (
	"context"

	"github.com/olianate/memo/internal/config"
)

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
	OpenURL(url string) error
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

// OpenURL opens an http(s) link in the system browser.
func (s *PlatformService) OpenURL(url string) error { return s.hooks.OpenURL(url) }

// CurrentTheme is read by the frontend before the first paint.
func (s *PlatformService) CurrentTheme() string {
	return s.cfg.Get(context.Background(), config.KeyTheme)
}
