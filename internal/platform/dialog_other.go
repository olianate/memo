//go:build !windows && !darwin && !linux

package platform

// ShowError is a no-op on platforms without a known native dialog. The message
// still reaches the log file via the caller.
func ShowError(string, string) {}
