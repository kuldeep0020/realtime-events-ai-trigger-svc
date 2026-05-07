package api

import (
	"encoding/json"
	"net/http"
)

// errorResponse is the consistent error shape returned by every endpoint:
//
//	{"error": "<message>"}
//
// All handlers route errors through writeError so the UI can rely on the
// shape regardless of which handler produced it.
type errorResponse struct {
	Error string `json:"error"`
}

// writeJSON serialises body as JSON, sets the appropriate header, and
// writes the response. Errors during encoding are logged via the standard
// http.ResponseWriter error path; we deliberately keep this dependency-free.
func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if body == nil {
		return
	}
	_ = json.NewEncoder(w).Encode(body)
}

// writeError writes a consistent {error: ...} JSON body with the given
// status code.
func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, errorResponse{Error: msg})
}

// decodeJSON decodes the request body into dst, returning a 400 on failure.
// Returns true if decoding succeeded; false if writeError was already
// called and the handler should return immediately.
func decodeJSON(w http.ResponseWriter, r *http.Request, dst any) bool {
	if r.Body == nil {
		writeError(w, http.StatusBadRequest, "missing request body")
		return false
	}
	defer r.Body.Close()
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return false
	}
	return true
}
