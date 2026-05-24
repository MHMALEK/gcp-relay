package server

import (
	"io/fs"
	"regexp"
	"strings"
	"testing"
)

// TestInspectorUIUsesRelativePaths guards the inspector UI against regressing
// to absolute fetch URLs. Absolute paths break when the relay is served
// behind a reverse-proxy prefix (e.g. tract-cli mounts it at
// /api/gcp-relay/) because the browser sends requests to the proxy root.
func TestInspectorUIUsesRelativePaths(t *testing.T) {
	data, err := fs.ReadFile(staticFiles, "static/index.html")
	if err != nil {
		t.Fatal(err)
	}
	html := string(data)

	// Reject `fetch('/...')` / `fetch("/...")` — that's the bug.
	bad := regexp.MustCompile(`fetch\(\s*['"]/`)
	if loc := bad.FindStringIndex(html); loc != nil {
		t.Fatalf("inspector UI contains a hard-coded absolute fetch path: %q", html[loc[0]:min(loc[0]+60, len(html))])
	}

	// Sanity check: the prefix-aware helper is present.
	if !strings.Contains(html, "API_BASE") {
		t.Fatal("expected API_BASE prefix logic in inspector UI")
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
