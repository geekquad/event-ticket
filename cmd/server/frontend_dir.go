package main

import (
	"os"
	"path/filepath"
)

// resolveFrontendDir returns a directory containing index.html, styles.css, and app.js.
// It checks FRONTEND_DIR, then walks up from the working directory so `go run .` works
// from either the repo root or cmd/server.
func resolveFrontendDir() string {
	if dir := os.Getenv("FRONTEND_DIR"); dir != "" {
		return dir
	}
	cwd, err := os.Getwd()
	if err != nil {
		return "frontend"
	}
	for dir := cwd; ; {
		candidate := filepath.Join(dir, "frontend")
		if st, err := os.Stat(filepath.Join(candidate, "index.html")); err == nil && !st.IsDir() {
			abs, err := filepath.Abs(candidate)
			if err != nil {
				return candidate
			}
			return abs
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return filepath.Join(cwd, "frontend")
}
