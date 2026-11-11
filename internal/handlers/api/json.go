package api

import (
	"encoding/json"
	"log/slog"
	"net/http"
)

// How this surface answers. One place, so a route cannot invent a second shape
// for an error and leave a consumer parsing two.

type errorBody struct {
	Error string `json:"error"`
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, errorBody{Error: message})
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	// Nothing here is a browser page, but a JSON body that a browser can be
	// talked into rendering is a browser page.
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(body); err != nil {
		// The status is already written, so this can only be reported, never
		// turned into a different answer.
		slog.Error("writing an api response failed", "error", err)
	}
}
