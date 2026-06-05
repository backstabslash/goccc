package main

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// fileVisitor is invoked once per matched .jsonl file with its project slug and
// session id (the latter derived from the path; empty for top-level files).
type fileVisitor func(path, projectSlug, sessionID string)

// dirWalker walks a Claude Code projects directory, applying the project-slug
// filter and modtime cutoff, and calls onFile for each surviving .jsonl file.
type dirWalker struct {
	projectsDir string
	matchedSlug string
	hasCutoff   bool
	cutoff      time.Time
	onFile      fileVisitor
	totalFiles  int
}

func (w *dirWalker) walk(path string, d fs.DirEntry, err error) error {
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: %v\n", err)
		return nil
	}

	if d.IsDir() {
		if path == w.projectsDir {
			return nil
		}
		if w.matchedSlug != "" {
			rel, _ := filepath.Rel(w.projectsDir, path)
			slug := strings.SplitN(rel, string(filepath.Separator), 2)[0]
			if slug != w.matchedSlug {
				return fs.SkipDir
			}
		}
		return nil
	}

	if !strings.HasSuffix(path, ".jsonl") {
		return nil
	}

	if w.hasCutoff {
		if info, err := d.Info(); err == nil && info.ModTime().Before(w.cutoff) {
			return nil
		}
	}

	rel, err := filepath.Rel(w.projectsDir, path)
	if err != nil {
		return nil
	}
	parts := strings.SplitN(rel, string(filepath.Separator), 2)
	projectSlug := parts[0]

	sessionID := ""
	if len(parts) > 1 {
		sessionParts := strings.SplitN(parts[1], string(filepath.Separator), 2)
		sessionID = strings.TrimSuffix(sessionParts[0], ".jsonl")
	}

	w.totalFiles++
	w.onFile(path, projectSlug, sessionID)
	return nil
}
