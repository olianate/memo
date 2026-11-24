package main

import (
	"bytes"
	"context"
	"embed"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"

	"github.com/olianate/memo/internal/attachments"
	"github.com/olianate/memo/internal/config"
	"github.com/olianate/memo/internal/domain"
	"github.com/olianate/memo/internal/mcpserver"
	"github.com/olianate/memo/internal/memoedit"
	"github.com/olianate/memo/internal/platform"
	"github.com/olianate/memo/internal/presence"
	"github.com/olianate/memo/internal/search"
	"github.com/olianate/memo/internal/services"
	"github.com/olianate/memo/internal/stats"
	"github.com/olianate/memo/internal/store"
	syncengine "github.com/olianate/memo/internal/sync"
)

//go:embed all:frontend/dist
var assets embed.FS

//go:embed build/appicon.png
var appIcon []byte

func main() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	dataDir, err := platform.EnsureDataDir()
	if err != nil {
		log.Fatalf("memo: data directory: %v", err)
	}
	st, err := store.Open(filepath.Join(dataDir, "memo.db"))
	if err != nil {
		log.Fatalf("memo: open database: %v", err)
	}
	defer st.Close()

	cfg := config.New(st)
	deviceID := cfg.Get(ctx, config.KeyDeviceID)
	if deviceID == "" {
		deviceID = domain.NewID()
		_ = cfg.Set(ctx, config.KeyDeviceID, deviceID)
	}
	if _, err := st.EnsureDevice(ctx, deviceID); err != nil {
		log.Fatalf("memo: device state: %v", err)
	}

	keychain := platform.Keychain{}
	attachmentsSvc := attachments.New(st, cfg, dataDir)
	syncSvc := syncengine.New(st, cfg, keychain, attachmentsSvc)
	syncSvc.SetDeviceID(deviceID)
	searchSvc := search.New(st)
	statsSvc := stats.New(st)

	var app *application.App
	emit := func(name string, data any) {
		if app != nil {
			app.Event.Emit(name, data)
		}
	}

	hooks := &appHooks{cfg: cfg}
	keeper := presence.New(st, attachmentsSvc, syncSvc.Hydrate, time.Now, emit)
	syncSvc.SetPresence(keeper)
	editor := memoedit.New(st, attachmentsSvc, keeper, func(id string) {
		emit("memos:changed", map[string]string{"id": id})
	})
	memoSvc := services.NewMemoService(st, attachmentsSvc, emit, editor, keeper)
	tagSvc := services.NewTagService(st, emit)
	searchBoundary := services.NewSearchService(searchSvc)
	statsBoundary := services.NewStatsService(statsSvc)
	mcpSvc := mcpserver.New(memoSvc, editor, searchBoundary)
	settingsSvc := services.NewSettingsService(st, cfg, dataDir, hooks, keychain)
	syncBoundary := services.NewSyncService(syncSvc, cfg, st, keychain, emit)
	platformSvc := services.NewPlatformService(hooks, cfg)
	updateSvc := services.NewUpdateService(hooks, emit)

	var tray *application.SystemTray
	syncSvc.OnEvent(func(name string, data any) {
		emit(name, data)
		if name == "sync:status" && tray != nil {
			if status, ok := data.(syncengine.Status); ok {
				applyTrayStatus(tray, status)
			}
		}
	})

	app = application.New(application.Options{
		Name:        "memo",
		Description: "Local-first memos with a Notion archive",
		Icon:        appIcon,
		SingleInstance: &application.SingleInstanceOptions{
			// Must match build/config.yml productIdentifier (and CFBundleIdentifier).
			UniqueID: "com.memo.app",
			OnSecondInstanceLaunch: func(application.SecondInstanceData) {
				hooks.ShowMainWindow()
			},
		},
		Services: []application.Service{
			application.NewService(memoSvc),
			application.NewService(tagSvc),
			application.NewService(searchBoundary),
			application.NewService(statsBoundary),
			application.NewService(settingsSvc),
			application.NewService(syncBoundary),
			application.NewService(platformSvc),
			application.NewService(updateSvc),
		},
		Assets: application.AssetOptions{
			Handler: application.AssetFileServerFS(assets),
		},
		Mac: application.MacOptions{
			ApplicationShouldTerminateAfterLastWindowClosed: false,
		},
		OnShutdown: func() {
			if x, y := hooks.stickyPosition(); x != 0 || y != 0 {
				_ = hooks.SaveStickyPosition(x, y)
			}
			_ = mcpSvc.Stop()
			cancel()
		},
	})

	mainWindow := app.Window.NewWithOptions(application.WebviewWindowOptions{
		Name:             "main",
		Title:            "memo",
		Width:            1180,
		Height:           820,
		MinWidth:         900,
		MinHeight:        600,
		BackgroundColour: application.NewRGB(246, 248, 249),
		URL:              "/",
	})
	sticky := app.Window.NewWithOptions(application.WebviewWindowOptions{
		Name:             "sticky",
		Title:            "memo — 快速记录",
		Width:            440,
		Height:           300,
		MinWidth:         360,
		MinHeight:        220,
		Frameless:        true,
		AlwaysOnTop:      true,
		HideOnEscape:     true,
		Hidden:           true,
		BackgroundType:   application.BackgroundTypeSolid,
		BackgroundColour: application.NewRGB(246, 248, 249),
		URL:              "/sticky.html",
		Windows: application.WindowsWindow{
			HiddenOnTaskbar: true,
		},
		Mac: application.MacWindow{
			WindowLevel:        application.MacWindowLevelFloating,
			CollectionBehavior: application.MacWindowCollectionBehaviorCanJoinAllSpaces | application.MacWindowCollectionBehaviorFullScreenAuxiliary,
			TitleBar:           application.MacTitleBarHidden,
		},
	})
	// Closing the main window must only hide it. Wails otherwise destroys the
	// window, and a destroyed WebviewWindow cannot be shown again from the tray.
	mainWindow.RegisterHook(events.Common.WindowClosing, func(e *application.WindowEvent) {
		mainWindow.Hide()
		e.Cancel()
	})

	hooks.app = app
	hooks.mainWindow = mainWindow
	hooks.sticky = sticky
	hooks.emit = emit
	hooks.mcp = mcpSvc

	// Smoke-test hook: show quick capture without relying on the global hotkey.
	// It runs after app.Run has started the window loop.
	if os.Getenv("MEMO_SHOW_STICKY") == "1" {
		go func() {
			time.Sleep(2 * time.Second)
			sticky.Show()
			sticky.Focus()
		}()
	}

	if _, saved, _ := st.Setting(ctx, config.KeyStickyX); saved {
		x := cfg.Int(ctx, config.KeyStickyX)
		y := cfg.Int(ctx, config.KeyStickyY)
		// Move first so GetScreen sees the monitor that owns the saved point,
		// then clamp inside that monitor's work area.
		sticky.SetPosition(x, y)
		sticky.SetPosition(clampSticky(sticky, x, y))
	}
	sticky.OnWindowEvent(events.Common.WindowDidMove, func(*application.WindowEvent) {
		hooks.saveSticky()
	})

	showSticky := func() {
		sticky.Show()
		sticky.Focus()
	}
	if err := hooks.applyHotkey(cfg.Get(ctx, config.KeyHotkey), showSticky); err != nil {
		log.Printf("memo: capture hotkey: %v", err)
	}

	mainWindow.OnWindowEvent(events.Common.WindowFocus, func(*application.WindowEvent) {
		syncSvc.SetActive(true)
		syncSvc.Wake()
	})
	mainWindow.OnWindowEvent(events.Common.WindowLostFocus, func(*application.WindowEvent) {
		syncSvc.SetActive(false)
	})
	sticky.OnWindowEvent(events.Common.WindowFocus, func(*application.WindowEvent) {
		syncSvc.SetActive(true)
		syncSvc.Wake()
	})
	sticky.OnWindowEvent(events.Common.WindowLostFocus, func(*application.WindowEvent) {
		syncSvc.SetActive(false)
	})

	tray = buildTray(app, hooks, syncBoundary, showSticky)
	applyTrayStatus(tray, syncSvc.Status(ctx))

	if cfg.Bool(ctx, config.KeyMCPEnabled) {
		port := cfg.Int(ctx, config.KeyMCPPort)
		if port < 1024 || port > 65535 {
			port = 8765
		}
		if err := mcpSvc.Start(port); err != nil {
			log.Printf("memo: MCP server: %v", err)
		}
	}

	syncSvc.Start(ctx)
	updateSvc.Start(ctx)
	err = app.Run()
	if err != nil {
		log.Fatal(err)
	}
}

type appHooks struct {
	app        *application.App
	mainWindow *application.WebviewWindow
	sticky     *application.WebviewWindow
	cfg        *config.Store
	emit       func(string, any)
	mcp        *mcpserver.Service

	pendingViewMu sync.Mutex
	pendingView   string
}

func (h *appHooks) ApplyHotkey(accelerator string) error {
	return h.applyHotkey(accelerator, func() {
		h.sticky.Show()
		h.sticky.Focus()
	})
}

func (h *appHooks) applyHotkey(accelerator string, callback func()) error {
	if h.app == nil {
		return nil
	}
	_ = h.app.GlobalShortcut.UnregisterAll()
	if err := h.app.GlobalShortcut.Register(accelerator, callback); err != nil {
		// Report the failure and try one documented fallback so capture still
		// has a global entry point.
		const fallback = "CmdOrCtrl+Alt+M"
		if accelerator != fallback {
			if fallbackErr := h.app.GlobalShortcut.Register(fallback, callback); fallbackErr == nil {
				_ = h.cfg.Set(context.Background(), config.KeyHotkeyError,
					fmt.Sprintf("快捷键 %s 注册失败（%v），已临时回退到 %s", accelerator, err, fallback))
				return nil
			}
		}
		_ = h.cfg.Set(context.Background(), config.KeyHotkeyError, fmt.Sprintf("快捷键 %s 注册失败：%v", accelerator, err))
		return err
	}
	_ = h.cfg.Set(context.Background(), config.KeyHotkeyError, "")
	return nil
}

func (h *appHooks) ApplyStartAtLogin(enabled bool) error {
	if h.app == nil {
		return nil
	}
	if enabled {
		return h.app.Autostart.Enable()
	}
	return h.app.Autostart.Disable()
}

func (h *appHooks) OpenPath(path string) error { return platform.RevealPath(path) }

func (h *appHooks) ApplyMCP(enabled bool, port int) error {
	if h.mcp == nil {
		return nil
	}
	if !enabled {
		return h.mcp.Stop()
	}
	return h.mcp.Start(port)
}

func (h *appHooks) MCPStatus() (bool, string, string) {
	if h.mcp == nil {
		return false, "", ""
	}
	return h.mcp.Status()
}

func (h *appHooks) OpenURL(url string) error {
	if h.app == nil {
		return nil
	}
	return h.app.Browser.OpenURL(url)
}

func (h *appHooks) ShowMainWindow() {
	if h.mainWindow == nil {
		return
	}
	// Focus() already restores a minimised window, but doing it explicitly
	// keeps the tray action reliable on Windows where SetForegroundWindow is
	// ignored for minimised windows.
	if h.mainWindow.IsMinimised() {
		h.mainWindow.UnMinimise()
	}
	h.mainWindow.Show()
	h.mainWindow.Focus()
}

func (h *appHooks) ShowSettings() {
	// Store the intent before showing the window: the frontend checks it when
	// it gains focus, so a tray click during startup is never lost.
	h.pendingViewMu.Lock()
	h.pendingView = "settings"
	h.pendingViewMu.Unlock()
	h.ShowMainWindow()
	if h.emit != nil {
		h.emit("ui:navigate", "settings")
	}
}

func (h *appHooks) TakePendingView() string {
	h.pendingViewMu.Lock()
	defer h.pendingViewMu.Unlock()
	view := h.pendingView
	h.pendingView = ""
	return view
}

func (h *appHooks) ShowStickyWindow() {
	h.sticky.Show()
	h.sticky.Focus()
}

func (h *appHooks) HideStickyWindow() { h.sticky.Hide() }

func (h *appHooks) Quit() { h.app.Quit() }

func (h *appHooks) stickyPosition() (int, int) {
	if h.sticky == nil {
		return 0, 0
	}
	return h.sticky.Position()
}

func (h *appHooks) SaveStickyPosition(x, y int) error {
	ctx := context.Background()
	if err := h.cfg.SetInt(ctx, config.KeyStickyX, x); err != nil {
		return err
	}
	return h.cfg.SetInt(ctx, config.KeyStickyY, y)
}

func (h *appHooks) StickyPosition() (int, int) {
	ctx := context.Background()
	return h.cfg.Int(ctx, config.KeyStickyX), h.cfg.Int(ctx, config.KeyStickyY)
}

func (h *appHooks) saveSticky() {
	x, y := h.sticky.Position()
	_ = h.SaveStickyPosition(x, y)
}

// clampSticky keeps a restored position inside the visible monitor work area.
func clampSticky(w *application.WebviewWindow, x, y int) (int, int) {
	screen, err := w.GetScreen()
	if err != nil || screen == nil {
		return x, y
	}
	width, height := w.Size()
	wa := screen.WorkArea
	maxX := wa.X + wa.Width - width
	maxY := wa.Y + wa.Height - height
	if x < wa.X {
		x = wa.X
	}
	if y < wa.Y {
		y = wa.Y
	}
	if maxX > wa.X && x > maxX {
		x = maxX
	}
	if maxY > wa.Y && y > maxY {
		y = maxY
	}
	return x, y
}

func buildTray(app *application.App, hooks *appHooks, syncBoundary *services.SyncService, showSticky func()) *application.SystemTray {
	tray := app.SystemTray.New()
	tray.SetTooltip("memo")
	tray.SetIcon(trayDot(34, 197, 94))
	// Left-click is the most direct way back to the app. Right-click keeps the
	// full menu, and double-click is treated the same as a single click.
	tray.OnClick(func() { hooks.ShowMainWindow() })
	tray.OnDoubleClick(func() { hooks.ShowMainWindow() })
	tray.OnRightClick(func() { tray.OpenMenu() })
	menu := application.NewMenu()
	menu.Add("显示主窗口").OnClick(func(*application.Context) { hooks.ShowMainWindow() })
	menu.Add("快速记录").OnClick(func(*application.Context) { showSticky() })
	menu.AddSeparator()
	menu.Add("立即同步").OnClick(func(*application.Context) {
		go func() {
			if err := syncBoundary.SyncNow(); err != nil {
				app.Event.Emit("sync:error", err.Error())
			}
		}()
	})
	menu.Add("暂停同步").OnClick(func(*application.Context) {
		status := syncBoundary.Status()
		_ = syncBoundary.SetPaused(!status.Paused)
	})
	menu.AddSeparator()
	menu.Add("设置").OnClick(func(*application.Context) {
		hooks.ShowSettings()
	})
	menu.Add("退出").OnClick(func(*application.Context) { app.Quit() })
	tray.SetMenu(menu)
	return tray
}

func applyTrayStatus(tray *application.SystemTray, status syncengine.Status) {
	label := "memo"
	switch {
	case !status.Enabled:
		label = "memo — 同步关闭"
		tray.SetIcon(trayDot(154, 163, 173))
	case status.Paused:
		label = "memo — 同步暂停"
		tray.SetIcon(trayDot(154, 163, 173))
	case status.State == domain.SyncError:
		label = "memo — 同步错误"
		tray.SetIcon(trayDot(220, 38, 38))
	case status.State == domain.SyncSyncing:
		label = "memo — 同步中"
		tray.SetIcon(trayDot(37, 99, 235))
	default:
		tray.SetIcon(trayDot(34, 197, 94))
	}
	if status.Pending > 0 && status.Enabled {
		label += fmt.Sprintf(" · %d 待同步", status.Pending)
	}
	tray.SetTooltip(label)
}

// trayDot renders a small solid status dot. Windows scales it for the tray.
func trayDot(r, g, b uint8) []byte {
	img := image.NewRGBA(image.Rect(0, 0, 32, 32))
	for y := 0; y < 32; y++ {
		for x := 0; x < 32; x++ {
			dx := float64(x) - 15.5
			dy := float64(y) - 15.5
			if dx*dx+dy*dy <= 14*14 {
				img.Set(x, y, color.RGBA{R: r, G: g, B: b, A: 255})
			}
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil
	}
	return buf.Bytes()
}
