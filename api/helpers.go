package api

import (
	"encoding/json"
	"net/http"
	"strconv"
)

// json200 writes a JSON 200 response.
func json200(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

// jsonErr writes a JSON error response.
func jsonErr(w http.ResponseWriter, code int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": msg})
}

// decodeBody decodes a JSON request body into v.
func decodeBody(r *http.Request, v any) error {
	defer r.Body.Close()
	return json.NewDecoder(r.Body).Decode(v)
}

// queryInt returns an integer query parameter or a default value.
func queryInt(r *http.Request, key string, def int) int {
	v := r.URL.Query().Get(key)
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return def
	}
	return n
}

// queryStr returns a string query parameter.
func queryStr(r *http.Request, key string) string {
	return r.URL.Query().Get(key)
}
