package services

// Version is the app version shown in Settings. Keep it in sync with the
// release tag (v1.0.0 -> 1.0.0). Release builds may override it with:
//
//	go build -ldflags "-X memo/internal/services.Version=1.2.3"
var Version = "1.0.0"
