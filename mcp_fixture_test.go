package main

import (
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
)

// copyDir recursively copies src into dst, preserving directory structure.
func copyDir(t *testing.T, src, dst string) {
	t.Helper()
	if err := filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			t.Fatal(err)
		}
		rel, _ := filepath.Rel(src, path)
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0755)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		return os.WriteFile(target, data, 0644)
	}); err != nil {
		t.Fatal(err)
	}
}

// setupMCPFixture copies testdata/mcp into a temp dir and rewrites the
// transcript cwd and claude.json project key to point at the temp project dir.
func setupMCPFixture(t *testing.T) (claudeDir, projectDir, transcriptPath string) {
	t.Helper()
	srcDir, _ := filepath.Abs("testdata/mcp")
	tmpDir := t.TempDir()
	copyDir(t, srcDir, tmpDir)

	claudeDir = filepath.Join(tmpDir, "home", ".claude")
	projectDir = filepath.Join(tmpDir, "project")
	transcriptPath = filepath.Join(claudeDir, "projects", "-Users-alice-myproject", "session.jsonl")

	escaped := filepath.ToSlash(projectDir)
	content := `{"type":"user","cwd":"` + escaped + `","message":{"role":"user","content":"hello"}}` + "\n"
	content += `{"type":"assistant","message":{"role":"assistant","model":"claude-opus-4-6","content":"hi"}}` + "\n"
	if err := os.WriteFile(transcriptPath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	claudeJSONPath := filepath.Join(tmpDir, "home", ".claude.json")
	rewriteClaudeJSONProjectKey(t, claudeJSONPath, "/Users/alice/myproject", projectDir)

	return claudeDir, projectDir, transcriptPath
}

// TestFixture_MCPDetection runs detectMCPs against the static testdata/mcp/
// fixture — a realistic home + project layout exercising all MCP sources:
//
//  1. ~/.claude/settings.json → mcpServers          ("settings-server")
//  2. ~/.claude/settings.json → enabledPlugins       ("github", "slack"; "disabled-plugin" off)
//     + ~/.claude/plugins/ walk for .mcp.json
//  3. <cwd>/.mcp.json                                ("project-db", "project-sentry")
//  4. <cwd>/.claude/settings.local.json → mcpServers ("local-dev-server")
//  5. ~/.claude.json top-level → mcpServers           ("user-global-notion", "user-global-linear")
//  6. ~/.claude.json → projects.<path>.mcpServers     ("local-scoped-stripe", "project-sentry" dup)
//  7. ~/.claude.json → projects.<path> disabled:
//     - disabledMcpServers:     ["plugin:slack:slack", "settings-server"]
//     - disabledMcpjsonServers: ["project-db"]
//
// After dedup and disabled filtering, expected result (sorted):
//
//	github, local-dev-server, local-scoped-stripe, project-sentry,
//	user-global-linear, user-global-notion
func TestFixture_MCPDetection(t *testing.T) {
	claudeDir, _, transcriptPath := setupMCPFixture(t)

	names := detectMCPs(claudeDir, transcriptPath)

	want := []string{
		"github",
		"local-dev-server",
		"local-scoped-stripe",
		"project-sentry",
		"user-global-linear",
		"user-global-notion",
	}

	if len(names) != len(want) {
		t.Fatalf("got %d MCPs %v, want %d %v", len(names), names, len(want), want)
	}
	for i := range want {
		if names[i] != want[i] {
			t.Errorf("names[%d] = %q, want %q", i, names[i], want[i])
		}
	}
}

// TestFixture_MCPDetection_DisabledVerification verifies the specific servers
// that should be filtered out by disabled lists.
func TestFixture_MCPDetection_DisabledVerification(t *testing.T) {
	claudeDir, _, transcriptPath := setupMCPFixture(t)

	names := detectMCPs(claudeDir, transcriptPath)

	nameSet := make(map[string]bool)
	for _, n := range names {
		nameSet[n] = true
	}

	shouldBeDisabled := []string{"slack", "settings-server", "project-db"}
	for _, d := range shouldBeDisabled {
		if nameSet[d] {
			t.Errorf("%q should be disabled but was present in results", d)
		}
	}
}

// rewriteClaudeJSONProjectKey rewrites ~/.claude.json to replace an old project
// key with a new one.
func rewriteClaudeJSONProjectKey(t *testing.T, path string, oldKey string, newKey string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatal(err)
	}

	var projects map[string]json.RawMessage
	if err := json.Unmarshal(raw["projects"], &projects); err != nil {
		t.Fatal(err)
	}

	newKeySlash := filepath.ToSlash(newKey)
	if cfg, ok := projects[oldKey]; ok {
		delete(projects, oldKey)
		projects[newKeySlash] = cfg
	}

	projectsData, _ := json.Marshal(projects)
	raw["projects"] = projectsData
	out, _ := json.MarshalIndent(raw, "", "  ")
	if err := os.WriteFile(path, out, 0644); err != nil {
		t.Fatal(err)
	}
}
