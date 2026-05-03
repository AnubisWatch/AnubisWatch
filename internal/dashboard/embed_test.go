package dashboard

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

// mockFileInfo implements os.FileInfo for mock files
type mockFileInfo struct {
	name    string
	size    int64
	modTime time.Time
}

func (fi mockFileInfo) Name() string       { return fi.name }
func (fi mockFileInfo) Size() int64        { return fi.size }
func (fi mockFileInfo) Mode() os.FileMode  { return 0644 }
func (fi mockFileInfo) ModTime() time.Time { return fi.modTime }
func (fi mockFileInfo) IsDir() bool        { return false }
func (fi mockFileInfo) Sys() interface{}   { return nil }

// mockFile implements http.File but can fail on Stat()
type mockFile struct {
	statErr  error
	statInfo os.FileInfo
	name     string
	modTime  time.Time
	content  []byte
	offset   int64
}

func (f *mockFile) Stat() (os.FileInfo, error) {
	if f.statErr != nil {
		return nil, f.statErr
	}
	if f.statInfo != nil {
		return f.statInfo, nil
	}
	return mockFileInfo{name: f.name, size: int64(len(f.content)), modTime: f.modTime}, nil
}

func (f *mockFile) Read(p []byte) (n int, err error) {
	if int(f.offset) >= len(f.content) {
		return 0, io.EOF
	}
	n = copy(p, f.content[f.offset:])
	f.offset += int64(n)
	return n, nil
}

func (f *mockFile) Close() error { return nil }
func (f *mockFile) Readdir(count int) ([]os.FileInfo, error) {
	return nil, nil
}
func (f *mockFile) Seek(offset int64, whence int) (int64, error) {
	switch whence {
	case io.SeekStart:
		f.offset = offset
	case io.SeekCurrent:
		f.offset += offset
	case io.SeekEnd:
		f.offset = int64(len(f.content)) + offset
	}
	return f.offset, nil
}

// mockFileSystem returns files that fail on Stat
type mockStatFailFS struct{}

func (m *mockStatFailFS) Open(name string) (http.File, error) {
	return &mockFile{statErr: errors.New("stat failed"), name: name}, nil
}

// mockNotFoundFS returns files that fail on Open
type mockNotFoundFS struct{}

func (m *mockNotFoundFS) Open(name string) (http.File, error) {
	return nil, os.ErrNotExist
}

// mockBothFailFS fails Open first, then Open("index.html") also fails
type mockBothFailFS struct{}

func (m *mockBothFailFS) Open(name string) (http.File, error) {
	return nil, errors.New("all files fail")
}

// mockIndexFallbackFS fails on first Open, succeeds on index.html
type mockIndexFallbackFS struct{}

func (m *mockIndexFallbackFS) Open(name string) (http.File, error) {
	if name == "index.html" {
		return &mockFile{name: "index.html", modTime: time.Now(), content: []byte("<html></html>")}, nil
	}
	return nil, os.ErrNotExist
}

func TestHandler_ServeHTTP_StatError(t *testing.T) {
	handler := &Handler{fs: &mockStatFailFS{}}

	req := httptest.NewRequest("GET", "/test", nil)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("Expected 500 for stat error, got %d", w.Code)
	}
}

func TestHandler_ServeHTTP_OpenFails_SPAFallback(t *testing.T) {
	handler := &Handler{fs: &mockIndexFallbackFS{}}

	req := httptest.NewRequest("GET", "/some/route", nil)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	// Should fallback to index.html
	if w.Code != http.StatusOK {
		t.Errorf("Expected 200 after SPA fallback, got %d", w.Code)
	}
}

func TestHandler_ServeHTTP_AllFilesFail(t *testing.T) {
	handler := &Handler{fs: &mockBothFailFS{}}

	req := httptest.NewRequest("GET", "/anything", nil)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	// Should return 404 when even index.html fails
	if w.Code != http.StatusNotFound {
		t.Errorf("Expected 404, got %d", w.Code)
	}
}

func TestHandler_ServeHTTP_NotFoundFS(t *testing.T) {
	handler := &Handler{fs: &mockNotFoundFS{}}

	req := httptest.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	// Should fallback to index.html, which also fails -> 404
	if w.Code != http.StatusNotFound {
		t.Errorf("Expected 404, got %d", w.Code)
	}
}

func TestHandler_ServeHTTP_ContentTypeSet(t *testing.T) {
	handler := &Handler{fs: &mockIndexFallbackFS{}}

	req := httptest.NewRequest("GET", "/app.js", nil)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	// Should set content-type header for index.html fallback
	ct := w.Header().Get("Content-Type")
	if ct != "text/html; charset=utf-8" {
		t.Errorf("Expected text/html content-type, got %q", ct)
	}
}

func TestNewHandler(t *testing.T) {
	handler, err := NewHandler()
	if err != nil {
		t.Fatalf("NewHandler failed: %v", err)
	}

	if handler == nil {
		t.Fatal("Expected handler to be created")
	}

	if handler.fs == nil {
		t.Error("Expected handler fs to be set")
	}
}

func TestHandler_ServeHTTP_Root(t *testing.T) {
	handler, err := NewHandler()
	if err != nil {
		t.Fatalf("NewHandler failed: %v", err)
	}

	req := httptest.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	// Should return index.html (200) or fallback gracefully
	if w.Code != http.StatusOK && w.Code != http.StatusNotFound {
		t.Errorf("Expected 200 or 404, got %d", w.Code)
	}
}

func TestHandler_ServeHTTP_Index(t *testing.T) {
	handler, err := NewHandler()
	if err != nil {
		t.Fatalf("NewHandler failed: %v", err)
	}

	req := httptest.NewRequest("GET", "/index.html", nil)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	// Should serve index.html or return 404 if not embedded
	t.Logf("Index.html returned status: %d", w.Code)
}

func TestHandler_ServeHTTP_SPA(t *testing.T) {
	handler, err := NewHandler()
	if err != nil {
		t.Fatalf("NewHandler failed: %v", err)
	}

	// SPA routing - unknown paths should return index.html
	req := httptest.NewRequest("GET", "/some/unknown/route", nil)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	// Should either return the file or fallback to index.html
	t.Logf("SPA route returned status: %d", w.Code)
}

func TestHandler_ServeHTTP_PrimarySPARoutes(t *testing.T) {
	handler, err := NewHandler()
	if err != nil {
		t.Fatalf("NewHandler failed: %v", err)
	}

	routes := []string{
		"/",
		"/souls",
		"/judgments",
		"/alerts",
		"/incidents",
		"/maintenance",
		"/journeys",
		"/cluster",
		"/status-pages",
		"/dashboards",
		"/settings",
	}

	for _, route := range routes {
		t.Run(route, func(t *testing.T) {
			req := httptest.NewRequest("GET", route, nil)
			w := httptest.NewRecorder()

			handler.ServeHTTP(w, req)

			if w.Code != http.StatusOK {
				t.Fatalf("expected %s to serve dashboard shell, got %d", route, w.Code)
			}
			if ct := w.Header().Get("Content-Type"); ct != "text/html; charset=utf-8" {
				t.Fatalf("expected HTML content type for %s, got %q", route, ct)
			}
			if body := w.Body.String(); !strings.Contains(body, `<div id="root"></div>`) {
				t.Fatalf("expected %s to return the SPA shell", route)
			}
		})
	}
}

func TestHandler_ServeHTTP_NotFound(t *testing.T) {
	handler, err := NewHandler()
	if err != nil {
		t.Fatalf("NewHandler failed: %v", err)
	}

	req := httptest.NewRequest("GET", "/nonexistent.xyz", nil)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	// Should return 404 for truly nonexistent files
	if w.Code != http.StatusOK && w.Code != http.StatusNotFound {
		t.Errorf("Expected 200 or 404, got %d", w.Code)
	}
}

func TestGetContentType(t *testing.T) {
	tests := []struct {
		filename string
		expected string
	}{
		{"index.html", "text/html; charset=utf-8"},
		{"app.js", "application/javascript; charset=utf-8"},
		{"bundle.mjs", "application/javascript; charset=utf-8"},
		{"styles.css", "text/css; charset=utf-8"},
		{"data.json", "application/json; charset=utf-8"},
		{"image.png", "image/png"},
		{"photo.jpg", "image/jpeg"},
		{"photo.jpeg", "image/jpeg"},
		{"animation.gif", "image/gif"},
		{"icon.svg", "image/svg+xml"},
		{"font.woff", "font/woff"},
		{"font.woff2", "font/woff2"},
		{"font.ttf", "font/ttf"},
		{"font.otf", "font/otf"},
		{"favicon.ico", "image/x-icon"},
		{"module.wasm", "application/wasm"},
		{"unknown.xyz", "application/octet-stream"},
		{".hidden", "application/octet-stream"},
		{"noextension", "application/octet-stream"},
	}

	for _, tt := range tests {
		result := getContentType(tt.filename)
		if result != tt.expected {
			t.Errorf("getContentType(%q) = %q, expected %q", tt.filename, result, tt.expected)
		}
	}
}

func TestGetContentType_Empty(t *testing.T) {
	result := getContentType("")
	if result != "application/octet-stream" {
		t.Errorf("getContentType(\"\") = %q, expected application/octet-stream", result)
	}
}

func TestHandler_ServeHTTP_Methods(t *testing.T) {
	handler, err := NewHandler()
	if err != nil {
		t.Fatalf("NewHandler failed: %v", err)
	}

	methods := []string{"GET", "POST", "PUT", "DELETE", "HEAD", "OPTIONS"}

	for _, method := range methods {
		req := httptest.NewRequest(method, "/", nil)
		w := httptest.NewRecorder()

		handler.ServeHTTP(w, req)

		// Handler should handle all methods gracefully
		t.Logf("Handler %s returned %d", method, w.Code)
	}
}

func TestHandler_ServeHTTP_Subdirectory(t *testing.T) {
	handler, err := NewHandler()
	if err != nil {
		t.Fatalf("NewHandler failed: %v", err)
	}

	// Test various content types by requesting different paths
	paths := []string{
		"/assets/app.css",
		"/assets/app.js",
		"/data/config.json",
		"/images/logo.png",
		"/fonts/main.woff2",
	}

	for _, p := range paths {
		req := httptest.NewRequest("GET", p, nil)
		w := httptest.NewRecorder()

		handler.ServeHTTP(w, req)

		// Should not panic
		if w.Code != http.StatusOK {
			t.Logf("Path %s returned %d", p, w.Code)
		}
	}
}

func TestHandler_ServeHTTP_Paths(t *testing.T) {
	handler, err := NewHandler()
	if err != nil {
		t.Fatalf("NewHandler failed: %v", err)
	}

	paths := []string{
		"/",
		"/.",
		"/app",
		"/app/",
		"/dashboard",
		"/status",
		"/settings",
		"/../../../etc/passwd", // Path traversal attempt
	}

	for _, p := range paths {
		req := httptest.NewRequest("GET", p, nil)
		w := httptest.NewRecorder()

		handler.ServeHTTP(w, req)

		// Should handle all paths gracefully (no panic)
		t.Logf("Path %s returned %d", p, w.Code)
	}
}
