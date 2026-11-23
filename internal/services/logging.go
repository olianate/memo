package services

import (
	"context"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/olianate/memo/internal/store"
)

// ErrorReport is a diagnostic event sent by the frontend. Every field is
// bounded and redacted before it reaches the local log, so pasting a log file
// into a bug report does not leak a Notion token.
type ErrorReport struct {
	Source  string `json:"source"`
	Message string `json:"message"`
	Stack   string `json:"stack,omitempty"`
	Context string `json:"context,omitempty"`
}

const (
	maxLogMessage = 2000
	maxLogStack   = 6000
	maxLogContext = 2000
	maxReportSize = 10000
)

var secretPatterns = []struct {
	re   *regexp.Regexp
	repl string
}{
	// Notion personal access tokens and legacy integration secrets.
	{regexp.MustCompile(`(?i)\b(?:ntn|secret)_[A-Za-z0-9_-]{6,}`), "[已隐藏令牌]"},
	// Authorization headers.
	{regexp.MustCompile(`(?i)\bauthorization\s*[:=]\s*[^\s,;]+`), "authorization: [已隐藏]"},
	{regexp.MustCompile(`(?i)\bbearer\s+[A-Za-z0-9._~+/=-]{6,}`), "Bearer [已隐藏]"},
	// token=..., "token":"...", password: ... and similar key/value pairs.
	{regexp.MustCompile(`(?i)\b(token|access_token|api_key|apikey|password|secret)\b\s*[:=]\s*("[^"]*"|'[^']*'|[^\s,;&]+)`), "$1=[已隐藏]"},
	// Token-like query parameters in copied URLs.
	{regexp.MustCompile(`(?i)([?&](?:token|access_token|api_key|apikey|secret)=)[^&\s]+`), "$1[已隐藏]"},
}

// redactSecrets removes the common ways a token can end up in an error string.
func redactSecrets(text string) string {
	for _, pattern := range secretPatterns {
		text = pattern.re.ReplaceAllString(text, pattern.repl)
	}
	return text
}

// truncateRunes bounds a value without cutting a UTF-8 sequence in half.
func truncateRunes(text string, limit int) string {
	if limit <= 0 || utf8.RuneCountInString(text) <= limit {
		return text
	}
	runes := []rune(text)
	return string(runes[:limit]) + "…(已截断)"
}

func sanitizeLogText(text string) string {
	return truncateRunes(strings.TrimSpace(redactSecrets(text)), maxLogMessage)
}

// sanitizeEventPart keeps the event column stable when a frontend source is
// free-form.
func sanitizeEventPart(text string) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return "frontend"
	}
	var b strings.Builder
	for _, r := range text {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '.', r == '-', r == '_':
			b.WriteRune(r)
		default:
			b.WriteRune('_')
		}
		if b.Len() >= 64 {
			break
		}
	}
	out := strings.Trim(b.String(), "_")
	if out == "" {
		return "frontend"
	}
	return out
}

// log records one bounded, redacted diagnostic event. Logging itself must never
// break a user action, so storage failures are deliberately ignored.
func (s *SyncService) log(level, event, message string) {
	if s == nil || s.store == nil {
		return
	}
	_ = s.store.Log(context.Background(), level, event, sanitizeLogText(message))
}

func (s *SyncService) logFailure(event string, err *error) {
	if err == nil || *err == nil {
		return
	}
	s.log("error", event, (*err).Error())
}

// ReportError stores a frontend diagnostic event. Source becomes part of the
// event name (`ui:<source>`), while stack and context stay in the message so a
// single exported log contains enough information to reproduce the issue.
func (s *SettingsService) ReportError(report ErrorReport) error {
	message := strings.TrimSpace(report.Message)
	if message == "" {
		message = "未知前端错误"
	}
	parts := []string{sanitizeLogText(message)}
	if stack := strings.TrimSpace(report.Stack); stack != "" {
		parts = append(parts, "stack:\n"+truncateRunes(redactSecrets(stack), maxLogStack))
	}
	if detail := strings.TrimSpace(report.Context); detail != "" {
		parts = append(parts, "context: "+truncateRunes(redactSecrets(detail), maxLogContext))
	}
	combined := truncateRunes(strings.Join(parts, "\n"), maxReportSize)
	event := "ui:" + sanitizeEventPart(report.Source)
	return s.store.Log(context.Background(), "error", event, combined)
}

// RecentErrors returns the most recent error-level entries for the settings UI.
func (s *SettingsService) RecentErrors() ([]store.SyncLog, error) {
	return s.store.Errors(context.Background(), 50)
}
