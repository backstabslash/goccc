package main

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// claudeSettings holds the fields we need from settings.json (read once).
type claudeSettings struct {
	MCPServers     map[string]json.RawMessage `json:"mcpServers"`
	EnabledPlugins map[string]bool            `json:"enabledPlugins"`
}

// claudeProjectConfig holds per-project MCP overrides from ~/.claude.json.
type claudeProjectConfig struct {
	DisabledMCPServers     []string `json:"disabledMcpServers"`
	DisabledMcpjsonServers []string `json:"disabledMcpjsonServers"`
}

func detectMCPs(claudeDir string, transcriptPath string) []string {
	settings := readSettings(filepath.Join(claudeDir, "settings.json"))

	var allNames []string
	for name := range settings.MCPServers {
		allNames = append(allNames, name)
	}
	allNames = append(allNames, parseEnabledPluginMCPs(settings.EnabledPlugins, filepath.Join(claudeDir, "plugins"))...)

	cwd := extractCwdFromTranscript(transcriptPath)
	allNames = append(allNames, parseProjectMCPsFromDir(cwd)...)

	claudeJSONPath := filepath.Join(filepath.Dir(claudeDir), ".claude.json")
	projects := readClaudeProjects(claudeJSONPath)

	projectPath := cwd
	if projectPath == "" {
		projectPath = projectPathFromSlug(projects, transcriptPath)
	}
	disabled := collectDisabledMCPs(findProject(projects, projectPath))

	return deduplicateAndSort(filterDisabled(allNames, disabled))
}

// findProject looks up a project config by path, normalizing slashes so that
// "C:\Users\foo" matches "C:/Users/foo" (Windows transcript vs .claude.json mismatch).
func findProject(projects map[string]claudeProjectConfig, path string) claudeProjectConfig {
	if cfg, ok := projects[path]; ok {
		return cfg
	}
	norm := strings.ReplaceAll(path, `\`, "/")
	for key, cfg := range projects {
		if strings.ReplaceAll(key, `\`, "/") == norm {
			return cfg
		}
	}
	return claudeProjectConfig{}
}

func readSettings(path string) claudeSettings {
	data, err := os.ReadFile(path)
	if err != nil {
		return claudeSettings{}
	}
	var s claudeSettings
	if err := json.Unmarshal(data, &s); err != nil {
		return claudeSettings{}
	}
	return s
}

func readClaudeProjects(claudeJSONPath string) map[string]claudeProjectConfig {
	data, err := os.ReadFile(claudeJSONPath)
	if err != nil {
		return nil
	}
	var config struct {
		Projects map[string]claudeProjectConfig `json:"projects"`
	}
	if err := json.Unmarshal(data, &config); err != nil {
		return nil
	}
	return config.Projects
}

func parseEnabledPluginMCPs(enabledPlugins map[string]bool, pluginsDir string) []string {
	enabled := make(map[string]bool)
	for key, on := range enabledPlugins {
		if !on {
			continue
		}
		parts := strings.SplitN(key, "@", 2)
		if len(parts) == 2 {
			enabled[parts[0]] = true
		}
	}
	if len(enabled) == 0 {
		return nil
	}

	remaining := len(enabled)
	found := make(map[string]bool)
	filepath.WalkDir(pluginsDir, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		if d.Name() != ".mcp.json" {
			return nil
		}
		dir := filepath.Dir(path)
		for dir != pluginsDir && dir != "." && dir != "/" {
			name := filepath.Base(dir)
			if enabled[name] && !found[name] {
				found[name] = true
				remaining--
				if remaining == 0 {
					return filepath.SkipAll
				}
				break
			}
			dir = filepath.Dir(dir)
		}
		return nil
	})

	var names []string
	for name := range found {
		names = append(names, name)
	}
	return names
}

func parseProjectMCPsFromDir(cwd string) []string {
	if cwd == "" {
		return nil
	}
	mcpPath := filepath.Join(cwd, ".mcp.json")
	data, err := os.ReadFile(mcpPath)
	if err != nil {
		return nil
	}
	var mcpConfig map[string]json.RawMessage
	if err := json.Unmarshal(data, &mcpConfig); err != nil {
		return nil
	}
	var names []string
	for name := range mcpConfig {
		names = append(names, name)
	}
	return names
}

// projectPathFromSlug derives the project path by matching the project slug
// in the transcript path against project keys.
// Slug format: /Users/foo/bar → -Users-foo-bar
func projectPathFromSlug(projects map[string]claudeProjectConfig, transcriptPath string) string {
	if transcriptPath == "" || len(projects) == 0 {
		return ""
	}
	slug := filepath.Base(filepath.Dir(transcriptPath))
	if slug == "" || slug == "." {
		return ""
	}
	for projectPath := range projects {
		if projectSlug(projectPath) == slug {
			return projectPath
		}
	}
	return ""
}

// projectSlug converts a project path to a Claude Code directory slug.
// Claude Code replaces "/", "\", and ":" with "-" when creating directory slugs.
func projectSlug(path string) string {
	r := strings.NewReplacer("/", "-", `\`, "-", ":", "-")
	return r.Replace(path)
}

func extractCwdFromTranscript(transcriptPath string) string {
	f, err := os.Open(transcriptPath)
	if err != nil {
		return ""
	}
	defer func() { _ = f.Close() }()

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for i := 0; i < 20 && scanner.Scan(); i++ {
		var entry struct {
			Cwd string `json:"cwd"`
		}
		if json.Unmarshal(scanner.Bytes(), &entry) == nil && entry.Cwd != "" {
			return entry.Cwd
		}
	}
	return ""
}

// collectDisabledMCPs extracts disabled MCP names from a project config.
// Entries can be plain names ("my-server") or plugin format ("plugin:<plugin>:<server>").
func collectDisabledMCPs(proj claudeProjectConfig) map[string]bool {
	all := append(proj.DisabledMCPServers, proj.DisabledMcpjsonServers...)
	if len(all) == 0 {
		return nil
	}
	disabled := make(map[string]bool, len(all))
	for _, entry := range all {
		if parts := strings.SplitN(entry, ":", 3); len(parts) == 3 && parts[0] == "plugin" {
			disabled[strings.ToLower(parts[1])] = true
		} else {
			disabled[strings.ToLower(entry)] = true
		}
	}
	return disabled
}

func filterDisabled(names []string, disabled map[string]bool) []string {
	if len(disabled) == 0 {
		return names
	}
	var filtered []string
	for _, n := range names {
		if !disabled[strings.ToLower(n)] {
			filtered = append(filtered, n)
		}
	}
	return filtered
}

func deduplicateAndSort(names []string) []string {
	seen := make(map[string]bool)
	var unique []string
	for _, n := range names {
		lower := strings.ToLower(n)
		if !seen[lower] {
			seen[lower] = true
			unique = append(unique, n)
		}
	}
	sort.Strings(unique)
	return unique
}
