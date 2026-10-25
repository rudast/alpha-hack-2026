package web_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/rudast/alpha-hack-2026/web"
)

func TestWebHandler_EmbeddedFallback(t *testing.T) {
	// Pass non-existent disk directory to force embedded FS
	h := web.Handler("/non/existent/path")

	cases := []struct {
		path string
	}{
		{"/"},
		{"/index.html"},
		{"/login/"},
		{"/personalAccount/"},
		{"/assets/css/core.css"},
	}

	for _, tc := range cases {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, tc.path, nil)
		h.ServeHTTP(rec, req)

		if tc.path == "/index.html" {
			// http.FileServer canonicalizes /index.html to ./ via 301 Moved Permanently
			if rec.Code != http.StatusMovedPermanently {
				t.Errorf("expected 301 for /index.html, got %d", rec.Code)
			}
			continue
		}

		if rec.Code != http.StatusOK {
			t.Errorf("expected 200 OK for %q, got %d", tc.path, rec.Code)
		}
		if rec.Body.Len() == 0 {
			t.Errorf("expected non-empty body for %q", tc.path)
		}
	}
}
