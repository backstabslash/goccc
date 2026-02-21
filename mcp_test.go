package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestReadSettings(t *testing.T) {
	tests := []struct {
		name            string
		json            string
		expectServers   int
		expectPlugins   int
	}{
		{"both fields", `{"mcpServers":{"a":{},"b":{}},"enabledPlugins":{"x@m":true}}`, 2, 1},
		{"servers only", `{"mcpServers":{"a":{}}}`, 1, 0},
		{"empty", `{}`, 0, 0},
		{"invalid json", `not json`, 0, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "settings.json")
			os.WriteFile(path, []byte(tt.json), 0644)
			s := readSettings(path)
			if len(s.MCPServers) != tt.expectServers {
				t.Errorf("MCPServers: got %d, want %d", len(s.MCPServers), tt.expectServers)
			}
			if len(s.EnabledPlugins) != tt.expectPlugins {
				t.Errorf("EnabledPlugins: got %d, want %d", len(s.EnabledPlugins), tt.expectPlugins)
			}
		})
	}
}

func TestReadSettings_MissingFile(t *testing.T) {
	s := readSettings("/nonexistent/settings.json")
	if len(s.MCPServers) != 0 || len(s.EnabledPlugins) != 0 {
		t.Errorf("expected empty for missing file")
	}
}

func TestParseEnabledPluginMCPs(t *testing.T) {
	dir := t.TempDir()
	pluginsDir := filepath.Join(dir, "plugins")

	enabled := map[string]bool{
		"github@official":   true,
		"slack@official":    true,
		"disabled@official": false,
		"skills@mymarket":   true,
	}

	githubDir := filepath.Join(pluginsDir, "cache", "official", "github", "1.0.0")
	os.MkdirAll(githubDir, 0755)
	os.WriteFile(filepath.Join(githubDir, ".mcp.json"), []byte(`{"github":{}}`), 0644)

	slackDir := filepath.Join(pluginsDir, "cache", "official", "slack", "1.0.0")
	os.MkdirAll(slackDir, 0755)
	os.WriteFile(filepath.Join(slackDir, ".mcp.json"), []byte(`{"slack":{}}`), 0644)

	names := parseEnabledPluginMCPs(enabled, pluginsDir)
	if len(names) != 2 {
		t.Errorf("expected 2 MCP plugins, got %d: %v", len(names), names)
	}
	found := map[string]bool{}
	for _, n := range names {
		found[n] = true
	}
	if !found["github"] || !found["slack"] {
		t.Errorf("expected github and slack, got %v", names)
	}
}

func TestParseEnabledPluginMCPs_AlternateLayout(t *testing.T) {
	dir := t.TempDir()
	pluginsDir := filepath.Join(dir, "plugins")
	enabled := map[string]bool{"github@official": true}

	githubDir := filepath.Join(pluginsDir, "marketplaces", "official", "external_plugins", "github")
	os.MkdirAll(githubDir, 0755)
	os.WriteFile(filepath.Join(githubDir, ".mcp.json"), []byte(`{}`), 0644)

	names := parseEnabledPluginMCPs(enabled, pluginsDir)
	if len(names) != 1 || names[0] != "github" {
		t.Errorf("expected [github] from old layout, got %v", names)
	}
}

func TestParseEnabledPluginMCPs_DisabledSkipped(t *testing.T) {
	dir := t.TempDir()
	pluginsDir := filepath.Join(dir, "plugins")
	enabled := map[string]bool{"github@official": false}

	githubDir := filepath.Join(pluginsDir, "cache", "official", "github", "1.0.0")
	os.MkdirAll(githubDir, 0755)
	os.WriteFile(filepath.Join(githubDir, ".mcp.json"), []byte(`{}`), 0644)

	names := parseEnabledPluginMCPs(enabled, pluginsDir)
	if len(names) != 0 {
		t.Errorf("disabled plugins should be skipped, got %v", names)
	}
}

func TestParseProjectMCPsFromDir(t *testing.T) {
	dir := t.TempDir()
	mcpConfig := map[string]interface{}{"my-server": map[string]string{"type": "stdio"}}
	mcpData, _ := json.Marshal(mcpConfig)
	os.WriteFile(filepath.Join(dir, ".mcp.json"), mcpData, 0644)

	names := parseProjectMCPsFromDir(dir)
	if len(names) != 1 || names[0] != "my-server" {
		t.Errorf("expected [my-server], got %v", names)
	}
}

func TestParseProjectMCPsFromDir_NoMcpFile(t *testing.T) {
	names := parseProjectMCPsFromDir(t.TempDir())
	if len(names) != 0 {
		t.Errorf("expected empty, got %v", names)
	}
}

func TestParseProjectMCPsFromDir_EmptyPath(t *testing.T) {
	names := parseProjectMCPsFromDir("")
	if len(names) != 0 {
		t.Errorf("expected empty for empty path, got %v", names)
	}
}

func TestProjectPathFromSlug(t *testing.T) {
	projects := map[string]claudeProjectConfig{
		"/Users/foo/myproject": {},
		"/Users/bar/other":    {},
	}

	result := projectPathFromSlug(projects, "/tmp/.claude/projects/-Users-foo-myproject/session.jsonl")
	if result != "/Users/foo/myproject" {
		t.Errorf("expected /Users/foo/myproject, got %q", result)
	}
}

func TestProjectPathFromSlug_Windows(t *testing.T) {
	projects := map[string]claudeProjectConfig{
		"C:/Users/foo/myproject": {DisabledMCPServers: []string{"plugin:ctx:ctx"}},
	}
	result := projectPathFromSlug(projects, "/tmp/.claude/projects/C--Users-foo-myproject/session.jsonl")
	if result != "C:/Users/foo/myproject" {
		t.Errorf("expected C:/Users/foo/myproject, got %q", result)
	}
}

func TestProjectPathFromSlug_NoMatch(t *testing.T) {
	projects := map[string]claudeProjectConfig{"/Users/foo/bar": {}}
	result := projectPathFromSlug(projects, "/tmp/projects/-Users-baz-qux/session.jsonl")
	if result != "" {
		t.Errorf("expected empty, got %q", result)
	}
}

func TestProjectPathFromSlug_EmptyInputs(t *testing.T) {
	if r := projectPathFromSlug(nil, "/some/path.jsonl"); r != "" {
		t.Errorf("expected empty for nil projects, got %q", r)
	}
	if r := projectPathFromSlug(map[string]claudeProjectConfig{"a": {}}, ""); r != "" {
		t.Errorf("expected empty for empty transcript, got %q", r)
	}
}

func TestFindProject(t *testing.T) {
	projects := map[string]claudeProjectConfig{
		"C:/Users/foo/myproject": {DisabledMCPServers: []string{"plugin:ctx:ctx"}},
	}
	tests := []struct {
		name, lookup string
		wantDisabled int
	}{
		{"exact match", "C:/Users/foo/myproject", 1},
		{"backslash lookup", `C:\Users\foo\myproject`, 1},
		{"no match", "C:/Users/other/project", 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := findProject(projects, tt.lookup)
			if len(cfg.DisabledMCPServers) != tt.wantDisabled {
				t.Errorf("findProject(%q) disabled=%d, want %d", tt.lookup, len(cfg.DisabledMCPServers), tt.wantDisabled)
			}
		})
	}
}

func TestProjectSlug(t *testing.T) {
	tests := []struct {
		name, path, expected string
	}{
		{"unix", "/Users/foo/bar", "-Users-foo-bar"},
		{"windows forward slash", "C:/Users/foo/bar", "C--Users-foo-bar"},
		{"windows backslash", `C:\Users\foo\bar`, "C--Users-foo-bar"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := projectSlug(tt.path)
			if got != tt.expected {
				t.Errorf("projectSlug(%q) = %q, want %q", tt.path, got, tt.expected)
			}
		})
	}
}

func TestExtractCwdFromTranscript(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "session.jsonl")

	content := `{"type":"summary","summary":"test"}
{"type":"user","cwd":"/home/user/project","message":{"role":"user"}}
{"type":"assistant","cwd":"/home/user/project","message":{"model":"claude-opus-4-6"}}
`
	os.WriteFile(path, []byte(content), 0644)

	cwd := extractCwdFromTranscript(path)
	if cwd != "/home/user/project" {
		t.Errorf("expected /home/user/project, got %q", cwd)
	}
}

func TestExtractCwdFromTranscript_NoCwd(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "session.jsonl")
	os.WriteFile(path, []byte(`{"type":"summary"}
`), 0644)

	cwd := extractCwdFromTranscript(path)
	if cwd != "" {
		t.Errorf("expected empty, got %q", cwd)
	}
}

func TestCollectDisabledMCPs(t *testing.T) {
	proj := claudeProjectConfig{
		DisabledMCPServers:     []string{"plugin:confluence:confluence", "my-server"},
		DisabledMcpjsonServers: []string{"project-db"},
	}
	disabled := collectDisabledMCPs(proj)
	if len(disabled) != 3 {
		t.Errorf("expected 3 disabled, got %d: %v", len(disabled), disabled)
	}
	if !disabled["confluence"] || !disabled["my-server"] || !disabled["project-db"] {
		t.Errorf("expected confluence, my-server, project-db disabled, got %v", disabled)
	}
}

func TestCollectDisabledMCPs_Empty(t *testing.T) {
	disabled := collectDisabledMCPs(claudeProjectConfig{})
	if len(disabled) != 0 {
		t.Errorf("expected empty, got %v", disabled)
	}
}

func TestFilterDisabled(t *testing.T) {
	names := []string{"confluence", "jira", "my-server"}
	disabled := map[string]bool{"confluence": true, "my-server": true}
	result := filterDisabled(names, disabled)
	if len(result) != 1 || result[0] != "jira" {
		t.Errorf("expected [jira], got %v", result)
	}
}

func TestFilterDisabled_NilMap(t *testing.T) {
	names := []string{"a", "b"}
	result := filterDisabled(names, nil)
	if len(result) != 2 {
		t.Errorf("expected pass-through with nil disabled, got %v", result)
	}
}

func TestDeduplicateAndSort(t *testing.T) {
	names := deduplicateAndSort([]string{"github", "slack", "GitHub", "jira", "Slack"})
	if len(names) != 3 {
		t.Errorf("expected 3 unique names, got %d: %v", len(names), names)
	}
	if names[0] != "github" || names[1] != "jira" || names[2] != "slack" {
		t.Errorf("expected [github jira slack], got %v", names)
	}
}

func TestDeduplicateAndSort_Empty(t *testing.T) {
	names := deduplicateAndSort(nil)
	if len(names) != 0 {
		t.Errorf("expected empty, got %v", names)
	}
}

func TestDetectMCPs_EmptyGraceful(t *testing.T) {
	names := detectMCPs("/nonexistent/path", "")
	if len(names) != 0 {
		t.Errorf("expected empty for missing paths, got %v", names)
	}
}

func TestDetectMCPs_CombinesSources(t *testing.T) {
	dir := t.TempDir()
	claudeDir := filepath.Join(dir, ".claude")
	pluginsDir := filepath.Join(claudeDir, "plugins")
	projectDir := filepath.Join(dir, "project")
	os.MkdirAll(projectDir, 0755)

	settings := map[string]interface{}{
		"mcpServers":     map[string]interface{}{"direct-server": map[string]string{}},
		"enabledPlugins": map[string]bool{"github@official": true, "disabled@official": false},
	}
	data, _ := json.Marshal(settings)
	os.MkdirAll(claudeDir, 0755)
	os.WriteFile(filepath.Join(claudeDir, "settings.json"), data, 0644)

	githubDir := filepath.Join(pluginsDir, "cache", "official", "github", "1.0.0")
	os.MkdirAll(githubDir, 0755)
	os.WriteFile(filepath.Join(githubDir, ".mcp.json"), []byte(`{"github":{}}`), 0644)

	transcriptPath := filepath.Join(dir, "session.jsonl")
	line := `{"type":"user","cwd":"` + filepath.ToSlash(projectDir) + `"}` + "\n"
	os.WriteFile(transcriptPath, []byte(line), 0644)

	mcpData, _ := json.Marshal(map[string]interface{}{"project-db": map[string]string{}})
	os.WriteFile(filepath.Join(projectDir, ".mcp.json"), mcpData, 0644)

	names := detectMCPs(claudeDir, transcriptPath)
	if len(names) != 3 {
		t.Errorf("expected 3 MCPs (direct-server, github, project-db), got %d: %v", len(names), names)
	}
	found := map[string]bool{}
	for _, n := range names {
		found[n] = true
	}
	if !found["direct-server"] || !found["github"] || !found["project-db"] {
		t.Errorf("missing expected MCP names: %v", names)
	}
}

func TestDetectMCPs_FiltersDisabled(t *testing.T) {
	dir := t.TempDir()
	claudeDir := filepath.Join(dir, ".claude")
	os.MkdirAll(claudeDir, 0755)

	settings := map[string]interface{}{
		"mcpServers": map[string]interface{}{
			"server-a": map[string]string{},
			"server-b": map[string]string{},
		},
	}
	sData, _ := json.Marshal(settings)
	os.WriteFile(filepath.Join(claudeDir, "settings.json"), sData, 0644)

	projectPath := "/test/project"
	claudeJSON := map[string]interface{}{
		"projects": map[string]interface{}{
			projectPath: map[string]interface{}{
				"disabledMcpServers": []string{"server-b"},
			},
		},
	}
	cData, _ := json.Marshal(claudeJSON)
	os.WriteFile(filepath.Join(dir, ".claude.json"), cData, 0644)

	// Transcript with cwd matching the project path
	transcriptPath := filepath.Join(dir, "session.jsonl")
	os.WriteFile(transcriptPath, []byte(`{"cwd":"`+projectPath+`"}`+"\n"), 0644)

	names := detectMCPs(claudeDir, transcriptPath)
	if len(names) != 1 || names[0] != "server-a" {
		t.Errorf("expected [server-a] after filtering, got %v", names)
	}
}
