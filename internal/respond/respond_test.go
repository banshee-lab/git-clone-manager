package respond

import (
	"net/http/httptest"
	"testing"
)

func TestErrorEnvelope(t *testing.T) {
	recorder := httptest.NewRecorder()
	Error(recorder, 404, "not_found", "missing")

	if recorder.Code != 404 {
		t.Fatalf("status = %d, want 404", recorder.Code)
	}
	want := "{\"error\":{\"code\":\"not_found\",\"message\":\"missing\"}}\n"
	if got := recorder.Body.String(); got != want {
		t.Fatalf("body = %q, want %q", got, want)
	}
}
