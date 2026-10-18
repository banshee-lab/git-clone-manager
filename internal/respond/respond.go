// Package respond writes the JSON response shapes used by HTTP handlers.
package respond

import (
	"encoding/json"
	"net/http"
)

type errorEnvelope struct {
	Error errorBody `json:"error"`
}

type errorBody struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// JSON writes value as a UTF-8 JSON response with the supplied status code.
func JSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

// Error writes the standard API error envelope.
func Error(w http.ResponseWriter, status int, code, message string) {
	JSON(w, status, errorEnvelope{Error: errorBody{Code: code, Message: message}})
}
