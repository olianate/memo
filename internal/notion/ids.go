package notion

import (
	"regexp"
	"strings"
)

var (
	notionDashedID = regexp.MustCompile(`(?i)[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}`)
	notionID       = regexp.MustCompile(`(?i)[0-9a-f]{32}`)
)

// ParsePageID accepts a raw Notion ID, a UUID, or a page URL and returns the
// compact 32-character ID. It does not verify that the ID belongs to a page.
func ParsePageID(input string) string {
	input = strings.TrimSpace(input)
	if input == "" {
		return ""
	}
	if match := notionDashedID.FindString(input); match != "" {
		return strings.ToLower(strings.ReplaceAll(match, "-", ""))
	}
	if match := notionID.FindString(input); match != "" {
		return strings.ToLower(match)
	}
	return ""
}
