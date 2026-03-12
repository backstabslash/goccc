package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestNormalizeVersion(t *testing.T) {
	tests := []struct {
		input, want string
	}{
		{"v1.2.3", "1.2.3"},
		{"1.2.3", "1.2.3"},
		{"v0.0.1", "0.0.1"},
		{"v0.1.2+dirty", "0.1.2"},
		{"1.0.0+build.123", "1.0.0"},
		{"v0.1.7-0.20260310152227-e2b82a9a71aa+dirty", "0.1.7"},
		{"v0.0.9-0.20260222164611-f6b538b59c20", "0.0.9"},
		{"v1.2.3-rc1", "1.2.3"},
		{"", ""},
	}
	for _, tt := range tests {
		if got := normalizeVersion(tt.input); got != tt.want {
			t.Errorf("normalizeVersion(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestIsNewer(t *testing.T) {
	tests := []struct {
		name            string
		latest, current string
		want            bool
	}{
		{"patch bump", "v0.0.9", "v0.0.8", true},
		{"minor bump", "v0.1.0", "v0.0.8", true},
		{"major bump", "v1.0.0", "v0.9.9", true},
		{"same version", "v0.1.0", "v0.1.0", false},
		{"current is newer patch", "v0.0.8", "v0.1.0", false},
		{"current is newer major", "v0.9.9", "v1.0.0", false},
		{"without v prefix", "0.2.0", "0.1.0", true},
		{"mixed prefix", "v0.2.0", "0.1.0", true},
		{"dirty suffix same version", "v0.1.2", "v0.1.2+dirty", false},
		{"dirty suffix older", "v0.1.3", "v0.1.2+dirty", true},
		{"pseudo-version same base", "v0.1.7", "v0.1.7-0.20260310152227-e2b82a9a71aa+dirty", false},
		{"pseudo-version newer release", "v0.2.0", "v0.1.7-0.20260310152227-e2b82a9a71aa+dirty", true},
		{"pseudo-version older release", "v0.1.6", "v0.1.7-0.20260310152227-e2b82a9a71aa+dirty", false},
		{"unparseable latest falls back to string compare", "abc", "v0.1.0", true},
		{"unparseable same", "abc", "abc", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isNewer(tt.latest, tt.current); got != tt.want {
				t.Errorf("isNewer(%q, %q) = %v, want %v", tt.latest, tt.current, got, tt.want)
			}
		})
	}
}

func TestPrintUpdateNoticeSuppressedForDev(t *testing.T) {
	origVersion := version
	version = "dev"
	defer func() { version = origVersion }()

	// Should not panic or print — just silently return
	printUpdateNotice(&updateResult{Latest: "v9.9.9", Stale: true})
}

func TestPrintUpdateNoticeShownForRelease(t *testing.T) {
	origVersion := version
	version = "v0.1.0"
	defer func() { version = origVersion }()

	r, w, _ := os.Pipe()
	origStderr := os.Stderr
	os.Stderr = w
	defer func() { os.Stderr = origStderr }()

	printUpdateNotice(&updateResult{Latest: "v0.2.0", Stale: true})
	_ = w.Close()

	buf := make([]byte, 1024)
	n, _ := r.Read(buf)
	if n == 0 {
		t.Error("expected update notice output for release version")
	}
}

func TestCacheReadFresh(t *testing.T) {
	cacheDir := t.TempDir()
	cacheFile := filepath.Join(cacheDir, "goccc", "latest-version")
	_ = os.MkdirAll(filepath.Dir(cacheFile), 0o755)

	content := time.Now().Format(time.RFC3339) + "\nv2.0.0\n"
	_ = os.WriteFile(cacheFile, []byte(content), 0o644)

	data, err := os.ReadFile(cacheFile)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != content {
		t.Errorf("cache content mismatch")
	}
}
