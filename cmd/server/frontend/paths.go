package frontend

import (
	"os"
	"path/filepath"
)

// ResolveDir returns the directory that contains index.html, styles.css, and app.js.
// Order: FRONTEND_DIR env, then walk parents of the working directory looking for
// cmd/server/frontend or frontend (supports `go run ./cmd/server` from repo root).
func ResolveDir() string {
	if dir := os.Getenv("FRONTEND_DIR"); dir != "" {
		return dir
	}
	cwd, err := os.Getwd()
	if err != nil {
		return "cmd/server/frontend"
	}
	candidates := []string{"cmd/server/frontend", "frontend"}
	for dir := cwd; ; {
		for _, rel := range candidates {
			root := filepath.Join(dir, rel)
			if st, err := os.Stat(filepath.Join(root, "index.html")); err == nil && !st.IsDir() {
				abs, err := filepath.Abs(root)
				if err != nil {
					return root
				}
				return abs
			}
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return filepath.Join(cwd, "cmd/server/frontend")
}
