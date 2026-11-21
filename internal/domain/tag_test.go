package domain

import "testing"

func TestExtractTagToken(t *testing.T) {
	tests := []struct {
		text  string
		caret int
		token string
		ok    bool
	}{
		{"hello #work", len("hello #work"), "work", true},
		{"#work", len("#work"), "work", true},
		{"中文 #工作", len("中文 #工作"), "工作", true},
		{"# Heading", len("#"), "", false},
		{"C#", len("C#"), "", false},
		{"hello#tag", len("hello#tag"), "", false},
		{"#tag with space", len("#tag with space"), "", false},
		{"acme #tag-one ", len("acme #tag-one"), "tag-one", true},
		{"`#code", len("`#code"), "", false},
	}
	for _, tt := range tests {
		token, _, _, ok := ExtractTagToken(tt.text, tt.caret)
		if ok != tt.ok || token != tt.token {
			t.Errorf("ExtractTagToken(%q, %d) = (%q, %v), want (%q, %v)", tt.text, tt.caret, token, ok, tt.token, tt.ok)
		}
	}
}

func TestRemoveTagToken(t *testing.T) {
	text := "hello #work world"
	start := len("hello ")
	end := start + len("#work")
	if got := RemoveTagToken(text, start, end); got != "hello world" {
		t.Fatalf("RemoveTagToken = %q", got)
	}
	if got := RemoveTagToken("#work", 0, 5); got != "" {
		t.Fatalf("leading token = %q", got)
	}
}

func TestNormalizeTag(t *testing.T) {
	if NormalizeTag("#Work") != "work" {
		t.Fatalf("normalize = %q", NormalizeTag("#Work"))
	}
	if CleanTagName("  #工作  ") != "工作" {
		t.Fatalf("clean = %q", CleanTagName("  #工作  "))
	}
}
