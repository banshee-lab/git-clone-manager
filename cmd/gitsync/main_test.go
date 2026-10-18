package main

import (
	"net/http/httptest"
	"testing"
)

func TestHealthz(t *testing.T) {
	recorder := httptest.NewRecorder()
	healthz(recorder, httptest.NewRequest("GET", "/healthz", nil))

	if recorder.Code != 200 {
		t.Fatalf("status = %d, want 200", recorder.Code)
	}
	if got, want := recorder.Body.String(), "{\"status\":\"ok\"}\n"; got != want {
		t.Fatalf("body = %q, want %q", got, want)
	}
}
