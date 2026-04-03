package dashboard

import (
	"embed"
	"io/fs"
	"net/http"
	"path"
	"strings"
)

//go:embed dist/*
var distFS embed.FS

// Handler serves the dashboard static files
type Handler struct {
	fs http.FileSystem
}

// NewHandler creates a new dashboard handler
func NewHandler() (*Handler, error) {
	// Create sub filesystem for dist directory
	subFS, err := fs.Sub(distFS, "dist")
	if err != nil {
		return nil, err
	}

	return &Handler{
		fs: http.FS(subFS),
	}, nil
}

// ServeHTTP implements http.Handler
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Clean the path
	filepath := path.Clean(r.URL.Path)
	if filepath == "/" || filepath == "." {
		filepath = "index.html"
	}

	// Remove leading slash
	filepath = strings.TrimPrefix(filepath, "/")

	// Try to open the file
	file, err := h.fs.Open(filepath)
	if err != nil {
		// If file not found, serve index.html (SPA routing)
		file, err = h.fs.Open("index.html")
		if err != nil {
			http.NotFound(w, r)
			return
		}
		filepath = "index.html"
	}
	defer file.Close()

	// Get file info
	stat, err := file.Stat()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Set content type based on extension
	contentType := getContentType(filepath)
	if contentType != "" {
		w.Header().Set("Content-Type", contentType)
	}

	// Serve the file
	http.ServeContent(w, r, filepath, stat.ModTime(), file)
}

func getContentType(filename string) string {
	switch path.Ext(filename) {
	case ".html":
		return "text/html; charset=utf-8"
	case ".js", ".mjs":
		return "application/javascript; charset=utf-8"
	case ".css":
		return "text/css; charset=utf-8"
	case ".json":
		return "application/json; charset=utf-8"
	case ".png":
		return "image/png"
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".gif":
		return "image/gif"
	case ".svg":
		return "image/svg+xml"
	case ".woff":
		return "font/woff"
	case ".woff2":
		return "font/woff2"
	case ".ttf":
		return "font/ttf"
	case ".otf":
		return "font/otf"
	case ".ico":
		return "image/x-icon"
	case ".wasm":
		return "application/wasm"
	default:
		return "application/octet-stream"
	}
}
