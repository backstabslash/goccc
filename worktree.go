package main

import (
	"os"
	"path/filepath"
	"strings"
)

// worktreeWalkLimit bounds the climb toward the filesystem root while looking
// for a repository marker, so an unusually deep cwd can't stall the statusline.
const worktreeWalkLimit = 64

// detectWorktree returns the directory name of the linked git worktree holding
// dir, or "" when dir sits in a main checkout, a submodule, or outside a
// repository. Detection is best-effort — any error yields "", never a failure.
//
// A linked worktree marks its root with a .git *file* pointing at
// "<repo>/.git/worktrees/<name>", while a main checkout has a .git directory.
// Submodules also use a .git file, but point into "<repo>/.git/modules/<name>".
func detectWorktree(dir string) string {
	for i := 0; i < worktreeWalkLimit && dir != ""; i++ {
		gitPath := filepath.Join(dir, ".git")
		info, err := os.Stat(gitPath)
		switch {
		case err != nil:
			// No marker at this level — keep walking up.
		case info.IsDir():
			return "" // main checkout
		case pointsAtWorktree(gitPath):
			return filepath.Base(dir)
		default:
			return "" // submodule, or a .git file we don't recognize
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			break // reached the filesystem root
		}
		dir = parent
	}
	return ""
}

// pointsAtWorktree reports whether a .git file's gitdir target names a linked
// worktree ("…/worktrees/<name>") rather than a submodule ("…/modules/<name>").
// The target may be absolute or relative, so only its last two elements matter.
func pointsAtWorktree(gitFile string) bool {
	data, err := os.ReadFile(gitFile)
	if err != nil {
		return false
	}
	target, ok := strings.CutPrefix(strings.TrimSpace(string(data)), "gitdir:")
	if !ok {
		return false
	}
	parts := strings.Split(filepath.ToSlash(strings.TrimSpace(target)), "/")
	return len(parts) >= 2 && parts[len(parts)-2] == "worktrees"
}
