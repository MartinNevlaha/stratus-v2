package api

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// allowedImageTypes maps MIME types to file extensions for image uploads.
var allowedImageTypes = map[string]string{
	"image/png":  ".png",
	"image/jpeg": ".jpg",
	"image/gif":  ".gif",
	"image/webp": ".webp",
}

func (s *Server) handleTerminalUploadImage(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 20<<20)

	if err := r.ParseMultipartForm(20 << 20); err != nil {
		jsonErr(w, http.StatusBadRequest, "invalid multipart: "+err.Error())
		return
	}

	file, header, err := r.FormFile("image")
	if err != nil {
		jsonErr(w, http.StatusBadRequest, "missing image file: "+err.Error())
		return
	}
	defer file.Close()

	// Resolve extension from Content-Type or original filename.
	ext := extFromMIME(header.Header.Get("Content-Type"))
	if ext == "" {
		ext = extFromFilename(header.Filename)
	}
	if ext == "" {
		jsonErr(w, http.StatusBadRequest, "unsupported image type")
		return
	}

	imageDir := filepath.Join(os.TempDir(), "stratus-images")
	if err := os.MkdirAll(imageDir, 0o755); err != nil {
		jsonErr(w, http.StatusInternalServerError, "create image dir: "+err.Error())
		return
	}

	id := randomID()
	filename := fmt.Sprintf("%s%s", id, ext)
	destPath := filepath.Join(imageDir, filename)

	dst, err := os.Create(destPath)
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, "create file: "+err.Error())
		return
	}
	defer dst.Close()

	if _, err := io.Copy(dst, file); err != nil {
		jsonErr(w, http.StatusInternalServerError, "write file: "+err.Error())
		return
	}

	json200(w, map[string]string{
		"path":     destPath,
		"filename": filename,
	})
}

func extFromMIME(ct string) string {
	if ext, ok := allowedImageTypes[ct]; ok {
		return ext
	}
	return ""
}

func extFromFilename(name string) string {
	e := strings.ToLower(filepath.Ext(name))
	if e == ".jpeg" {
		e = ".jpg"
	}
	for _, v := range allowedImageTypes {
		if v == e {
			return e
		}
	}
	return ""
}

func randomID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
