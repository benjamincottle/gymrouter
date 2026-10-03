package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// A long request (suggesting lines) extends its own write deadline. That has to reach the real connection
// through the logging wrapper, or the server's write timeout cuts the response and the client sees
// "Failed to fetch".
func TestWrapperExposesTheRealWriter(t *testing.T) {
	var sw http.ResponseWriter = &statusWriter{ResponseWriter: httptest.NewRecorder(), status: 200}
	rc := http.NewResponseController(sw)
	if err := rc.Flush(); err != nil {
		t.Errorf("Flush through statusWriter: %v", err)
	}
	// httptest.ResponseRecorder has no deadlines, so the only acceptable failure is "not supported" from the
	// underlying writer, never from our wrapper hiding it.
	err := rc.SetWriteDeadline(time.Now().Add(time.Minute))
	if err == nil {
		return
	}
	if _, ok := sw.(interface{ Unwrap() http.ResponseWriter }); !ok {
		t.Errorf("statusWriter must implement Unwrap: %v", err)
	}
}
