package main

import (
	"os"
	"path/filepath"
	"testing"
)

// newWorktree builds a linked worktree named name under a temp dir and returns
// its root — the same layout `git worktree add` produces.
func newWorktree(t *testing.T, name string) string {
	t.Helper()
	base := t.TempDir()

	repo := filepath.Join(base, "repo")
	gitDir := filepath.Join(repo, ".git")
	if err := os.MkdirAll(filepath.Join(gitDir, "worktrees", name), 0o755); err != nil {
		t.Fatal(err)
	}

	root := filepath.Join(base, name)
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	gitFile := "gitdir: " + filepath.Join(gitDir, "worktrees", name) + "\n"
	if err := os.WriteFile(filepath.Join(root, ".git"), []byte(gitFile), 0o644); err != nil {
		t.Fatal(err)
	}
	return root
}

// newGitFileDir writes a directory whose .git is a file with the given content.
func newGitFileDir(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ".git"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestDetectWorktree_LinkedWorktree(t *testing.T) {
	root := newWorktree(t, "feature-auth")
	if got := detectWorktree(root); got != "feature-auth" {
		t.Errorf("got %q, want feature-auth", got)
	}
}

func TestDetectWorktree_SubdirectoryOfWorktree(t *testing.T) {
	root := newWorktree(t, "hotfix")
	nested := filepath.Join(root, "cmd", "server")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	if got := detectWorktree(nested); got != "hotfix" {
		t.Errorf("got %q, want hotfix", got)
	}
}

func TestDetectWorktree_MainCheckout(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if got := detectWorktree(dir); got != "" {
		t.Errorf("got %q, want empty for a main checkout", got)
	}
}

// A worktree nested inside its own main checkout must resolve to the worktree,
// not stop at the parent repo — the layout Claude Code's .claude/worktrees uses.
func TestDetectWorktree_NestedInsideMainCheckout(t *testing.T) {
	repo := t.TempDir()
	if err := os.MkdirAll(filepath.Join(repo, ".git", "worktrees", "perf"), 0o755); err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(repo, ".claude", "worktrees", "perf")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	gitFile := "gitdir: " + filepath.Join(repo, ".git", "worktrees", "perf") + "\n"
	if err := os.WriteFile(filepath.Join(root, ".git"), []byte(gitFile), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := detectWorktree(root); got != "perf" {
		t.Errorf("got %q, want perf", got)
	}
}

func TestDetectWorktree_Submodule(t *testing.T) {
	dir := newGitFileDir(t, "gitdir: /repo/.git/modules/vendor/lib\n")
	if got := detectWorktree(dir); got != "" {
		t.Errorf("got %q, want empty for a submodule", got)
	}
}

func TestDetectWorktree_RelativeGitdir(t *testing.T) {
	dir := newGitFileDir(t, "gitdir: ../repo/.git/worktrees/rel\n")
	if got := detectWorktree(dir); got != filepath.Base(dir) {
		t.Errorf("got %q, want %q", got, filepath.Base(dir))
	}
}

func TestDetectWorktree_NotARepo(t *testing.T) {
	if got := detectWorktree(t.TempDir()); got != "" {
		t.Errorf("got %q, want empty outside a repo", got)
	}
}

func TestDetectWorktree_EmptyDir(t *testing.T) {
	if got := detectWorktree(""); got != "" {
		t.Errorf("got %q, want empty", got)
	}
}

func TestDetectWorktree_MalformedGitFile(t *testing.T) {
	for _, content := range []string{"", "not a gitdir line\n", "gitdir:\n", "gitdir: worktrees\n"} {
		dir := newGitFileDir(t, content)
		if got := detectWorktree(dir); got != "" {
			t.Errorf("content %q: got %q, want empty", content, got)
		}
	}
}

func TestRenderWorktree(t *testing.T) {
	root := newWorktree(t, "feature-auth")

	input := &StatuslineInput{Cwd: root}
	ctx := &StatuslineContext{Input: input}
	if got := renderWorktree(ctx); got != "🌳 feature-auth" {
		t.Errorf("got %q, want 🌳 feature-auth", got)
	}
}

func TestRenderWorktree_EmojiOverride(t *testing.T) {
	root := newWorktree(t, "feature-auth")

	ctx := &StatuslineContext{
		Input:   &StatuslineInput{Cwd: root},
		Options: map[string]SegmentOptions{"worktree": {Emoji: "🪵"}},
	}
	if got := renderWorktree(ctx); got != "🪵 feature-auth" {
		t.Errorf("got %q, want 🪵 feature-auth", got)
	}
}

func TestRenderWorktree_FallsBackToWorkspaceDir(t *testing.T) {
	root := newWorktree(t, "docs")

	input := &StatuslineInput{}
	input.Workspace.CurrentDir = root
	if got := renderWorktree(&StatuslineContext{Input: input}); got != "🌳 docs" {
		t.Errorf("got %q, want 🌳 docs", got)
	}
}

func TestRenderWorktree_HidesOutsideWorktree(t *testing.T) {
	ctx := &StatuslineContext{Input: &StatuslineInput{Cwd: t.TempDir()}}
	if got := renderWorktree(ctx); got != "" {
		t.Errorf("got %q, want empty", got)
	}
}

func TestAssembleStatusline_WorktreeSegment(t *testing.T) {
	root := newWorktree(t, "feature-auth")

	ctx := &StatuslineContext{Input: &StatuslineInput{Cwd: root}}
	got := assembleStatusline([]string{"cwd", "worktree"}, " · ", ctx)
	want := "📁 feature-auth · 🌳 feature-auth"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}
