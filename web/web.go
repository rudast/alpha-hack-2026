package web

import (
	"embed"
	"net/http"
	"os"
	"path/filepath"
)

//go:embed index.html assets login personalAccount
var embeddedFS embed.FS

// Handler returns an http.Handler that serves web assets.
// It prioritizes the disk directory if index.html is present there,
// and falls back to the embedded filesystem (needed on Vercel / serverless).
func Handler(diskDir string) http.Handler {
	if diskDir != "" {
		testFile := filepath.Join(diskDir, "index.html")
		if _, err := os.Stat(testFile); err == nil {
			return http.FileServer(http.Dir(diskDir))
		}
	}
	return http.FileServer(http.FS(embeddedFS))
}
