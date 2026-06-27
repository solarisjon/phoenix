package api

import (
	"encoding/json"
	"log/slog"
	"net/http"
)

// errResponse is the standard error envelope.
type errResponse struct {
	Error string `json:"error"`
}

// respond writes a JSON response with the given status code.
func respond(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if data == nil {
		return
	}
	if err := json.NewEncoder(w).Encode(data); err != nil {
		slog.Error("api: encode response", "error", err)
	}
}

// respondErr writes a JSON error response.
func respondErr(w http.ResponseWriter, status int, msg string) {
	respond(w, status, errResponse{Error: msg})
}

// respondInternalErr logs the error and returns a generic 500.
func respondInternalErr(w http.ResponseWriter, err error) {
	slog.Error("api: internal error", "error", err)
	respondErr(w, http.StatusInternalServerError, "internal server error")
}

// decode deserialises the request body into v.
// Returns false and writes 400 if decoding fails.
func decode(w http.ResponseWriter, r *http.Request, v any) bool {
	if err := json.NewDecoder(r.Body).Decode(v); err != nil {
		respondErr(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return false
	}
	return true
}
