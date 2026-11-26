package main

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/olianate/memo/internal/store"
)

func TestErrorDetailsIncludesLogPath(t *testing.T) {
	got := errorDetails(`C:\Users\me\AppData\Roaming\memo\logs\memo.log`, errors.New("open database: refused"))
	if !strings.Contains(got, "open database: refused") {
		t.Fatalf("missing error text: %q", got)
	}
	if !strings.Contains(got, `logs\memo.log`) {
		t.Fatalf("missing log path: %q", got)
	}
}

func TestErrorDetailsOmitsMissingLogPath(t *testing.T) {
	got := errorDetails("", errors.New("boom"))
	if strings.Contains(got, "日志：") {
		t.Fatalf("unexpected log hint: %q", got)
	}
}

func TestErrorDetailsTruncatesLongError(t *testing.T) {
	got := errorDetails("", errors.New(strings.Repeat("x", 4000)))
	runes := []rune(got)
	if len(runes) != 1501 {
		t.Fatalf("runes = %d, want 1501", len(runes))
	}
	if !strings.HasSuffix(got, "…") {
		t.Fatalf("missing ellipsis: %q", got[len(got)-5:])
	}
}

func TestFatalMessageExplainsLegacyProfile(t *testing.T) {
	err := fmt.Errorf("open database: %w", store.ErrLegacyProfile)
	got := fatalMessage("", err)
	if !strings.Contains(got, "改名或移走") {
		t.Fatalf("missing reset hint: %q", got)
	}
	if !strings.Contains(got, "block JSON") {
		t.Fatalf("missing original error: %q", got)
	}
}
