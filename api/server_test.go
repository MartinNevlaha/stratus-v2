package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/MartinNevlaha/stratus-v2/config"
)

func TestSpaHandler_DevMode(t *testing.T) {
	indexFS := fstest.MapFS{
		"index.html": &fstest.MapFile{Data: []byte("<html>stratus</html>")},
	}

	t.Run("dev mode returns 404 with dev mode message", func(t *testing.T) {
		s := &Server{
			cfg:         &config.Config{DevMode: true},
			staticFiles: fstest.MapFS{},
		}
		handler := s.spaHandler()
		req := httptest.NewRequest(http.MethodGet, "/memory", nil)
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)
		if rr.Code != http.StatusNotFound {
			t.Errorf("expected 404, got %d", rr.Code)
		}
		if !strings.Contains(rr.Body.String(), "dev mode") {
			t.Errorf("expected body to contain 'dev mode', got %q", rr.Body.String())
		}
	})

	t.Run("non-dev mode falls back to index.html for unknown path", func(t *testing.T) {
		s := &Server{
			cfg:         &config.Config{DevMode: false},
			staticFiles: indexFS,
		}
		handler := s.spaHandler()
		req := httptest.NewRequest(http.MethodGet, "/memory", nil)
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)
		if rr.Code != http.StatusOK {
			t.Errorf("expected 200, got %d", rr.Code)
		}
		if !strings.Contains(rr.Body.String(), "stratus") {
			t.Errorf("expected body to contain 'stratus', got %q", rr.Body.String())
		}
	})

	t.Run("nil cfg falls back to index.html without panic", func(t *testing.T) {
		s := &Server{
			cfg:         nil,
			staticFiles: indexFS,
		}
		handler := s.spaHandler()
		req := httptest.NewRequest(http.MethodGet, "/memory", nil)
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)
		if rr.Code != http.StatusOK {
			t.Errorf("expected 200, got %d", rr.Code)
		}
	})
}
