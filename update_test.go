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
		{"", ""},
	}
	for _, tt := range tests {
		if got := normalizeVersion(tt.input); got != tt.want {
			t.Errorf("normalizeVersion(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestIsDevVersion(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		{"dev", true},
		{"v0.0.0", true},
		{"v0.0.9-0.20260222164611-f6b538b59c20", true},
		{"0.0.9-0.20260222164611-f6b538b59c20", true},
		{"v1.2.3-rc1", true},
		{"v0.1.0", false},
		{"v0.0.8", false},
		{"1.2.3", false},
	}
	for _, tt := range tests {
		if got := isDevVersion(tt.input); got != tt.want {
			t.Errorf("isDevVersion(%q) = %v, want %v", tt.input, got, tt.want)
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

func TestCheckForUpdateSkipsDevVersion(t *testing.T) {
	for _, v := range []string{"dev", "v0.0.9-0.20260222164611-f6b538b59c20"} {
		ch := checkForUpdate(v)
		res := <-ch
		if res != nil {
			t.Errorf("expected nil for %q, got %+v", v, res)
		}
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
