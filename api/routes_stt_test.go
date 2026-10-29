package api

import (
	"bytes"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/MartinNevlaha/stratus-v2/config"
)

// sttUpstream records the multipart form fields the proxy forwards to speaches.
func sttUpstream(t *testing.T) (*httptest.Server, *map[string]string) {
	t.Helper()
	fields := map[string]string{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			t.Errorf("upstream parse multipart: %v", err)
		}
		for k, v := range r.MultipartForm.Value {
			fields[k] = v[0]
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"text":"ahoj"}`))
	}))
	t.Cleanup(srv.Close)
	return srv, &fields
}

func sttRequest(t *testing.T) *http.Request {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	fw, err := mw.CreateFormFile("file", "audio.webm")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = fw.Write([]byte("fake audio"))
	_ = mw.WriteField("model", "whisper-1")
	mw.Close()
	req := httptest.NewRequest(http.MethodPost, "/api/stt/transcribe", &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	return req
}

func TestSTTTranscribe_SendsConfiguredLanguage(t *testing.T) {
	upstream, fields := sttUpstream(t)
	cfg := config.Default()
	s := &Server{sttEndpoint: upstream.URL, sttModel: "Systran/faster-whisper-large-v3", cfg: &cfg}

	w := httptest.NewRecorder()
	s.handleSTTTranscribe(w, sttRequest(t))

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	if got := (*fields)["language"]; got != "sk" {
		t.Errorf("language = %q, want %q", got, "sk")
	}
	if got := (*fields)["model"]; got != "Systran/faster-whisper-large-v3" {
		t.Errorf("model = %q", got)
	}
}

func TestSTTTranscribe_EmptyLanguageOmitsField(t *testing.T) {
	upstream, fields := sttUpstream(t)
	cfg := config.Default()
	cfg.STT.Language = ""
	s := &Server{sttEndpoint: upstream.URL, sttModel: "m", cfg: &cfg}

	w := httptest.NewRecorder()
	s.handleSTTTranscribe(w, sttRequest(t))

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	if _, ok := (*fields)["language"]; ok {
		t.Errorf("language field sent, want auto-detect (no field)")
	}
}
