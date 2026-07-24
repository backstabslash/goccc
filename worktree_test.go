package main

import (
	"os"
	"path/filepath"
	"testing"
)

// linkWorktree wires a worktree root to its admin directory the way
// `git worktree add` does: a .git file at the root naming target, and the
// "gitdir" backlink git keeps inside the admin directory pointing back at it.
func linkWorktree(t *testing.T, root, adminDir, target string) {
	t.Helper()
	if err := os.MkdirAll(adminDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	gitFile := filepath.Join(root, ".git")
	if err := os.WriteFile(gitFile, []byte("gitdir: "+target+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(adminDir, "gitdir"), []byte(gitFile+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

// newWorktree builds a linked worktree named name under a temp dir and returns
// its root — the same layout `git worktree add` produces.
func newWorktree(t *testing.T, name string) string {
	t.Helper()
	base := t.TempDir()
	adminDir := filepath.Join(base, "repo", ".git", "worktrees", name)
	root := filepath.Join(base, name)
	linkWorktree(t, root, adminDir, adminDir)
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
	if err := os.MkdirAll(filepath.Join(repo, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	adminDir := filepath.Join(repo, ".git", "worktrees", "perf")
	root := filepath.Join(repo, ".claude", "worktrees", "perf")
	linkWorktree(t, root, adminDir, adminDir)

	// Start below the worktree root, so the walk has to stop at the worktree's
	// .git file rather than continue to the enclosing checkout's .git directory.
	nested := filepath.Join(root, "internal", "cache")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	if got := detectWorktree(nested); got != "perf" {
		t.Errorf("got %q, want perf", got)
	}
}

// A worktree of a bare repo has no ".git" element in its target at all — the
// admin directory sits directly under the bare repo.
func TestDetectWorktree_BareRepoParent(t *testing.T) {
	base := t.TempDir()
	adminDir := filepath.Join(base, "repo.git", "worktrees", "release")
	root := filepath.Join(base, "release")
	linkWorktree(t, root, adminDir, adminDir)
	if got := detectWorktree(root); got != "release" {
		t.Errorf("got %q, want release", got)
	}
}

// A target that merely happens to sit under a directory named "worktrees" is
// not a worktree — git's backlink is absent.
func TestDetectWorktree_WorktreesLookalike(t *testing.T) {
	base := t.TempDir()
	if err := os.MkdirAll(filepath.Join(base, "worktrees", "foo"), 0o755); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(base, "project")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".git"), []byte("gitdir: ../worktrees/foo\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := detectWorktree(dir); got != "" {
		t.Errorf("got %q, want empty for a lookalike target", got)
	}
}

func TestDetectWorktree_Submodule(t *testing.T) {
	dir := newGitFileDir(t, "gitdir: /repo/.git/modules/vendor/lib\n")
	if got := detectWorktree(dir); got != "" {
		t.Errorf("got %q, want empty for a submodule", got)
	}
}

func TestDetectWorktree_RelativeGitdir(t *testing.T) {
	base := t.TempDir()
	adminDir := filepath.Join(base, "repo", ".git", "worktrees", "rel")
	root := filepath.Join(base, "rel")
	linkWorktree(t, root, adminDir, "../repo/.git/worktrees/rel")
	if got := detectWorktree(root); got != "rel" {
		t.Errorf("got %q, want rel", got)
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
