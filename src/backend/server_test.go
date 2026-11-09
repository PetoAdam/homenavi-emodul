package backend

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
)

func TestWidgetAssetsAreNotCached(t *testing.T) {
	server := &Server{WebFS: fstest.MapFS{
		"widgets/configure/index.html":       &fstest.MapFile{Data: []byte("widget")},
		"widgets/configure/assets/widget.js": &fstest.MapFile{Data: []byte("widget script")},
		"assets/icon.png":                    &fstest.MapFile{Data: []byte("icon")},
	}}
	for _, path := range []string{"/widgets/configure/", "/widgets/configure/assets/widget.js", "/assets/icon.png"} {
		response := httptest.NewRecorder()
		server.Routes().ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, nil))
		if response.Code != http.StatusOK || !strings.Contains(response.Header().Get("Cache-Control"), "no-store") {
			t.Errorf("path=%s status=%d cache-control=%q", path, response.Code, response.Header().Get("Cache-Control"))
		}
	}
}
