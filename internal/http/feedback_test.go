package http

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestFeedbackHandler_DisabledWithoutDB(t *testing.T) {
	t.Parallel()
	h := NewFeedbackHandler(nil)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/feedback", bytes.NewReader([]byte(`{}`)))
	h.handlePost(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d", rec.Code)
	}
}
