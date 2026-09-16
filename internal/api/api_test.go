package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestExecuteRejectsEmptyCommand(t *testing.T) {
	s := &Server{Token: "test-token"}
	req := httptest.NewRequest(http.MethodPost, "/api/devices/router-1/execute", strings.NewReader(`{"command":"   "}`))
	req.Header.Set("Authorization", "Bearer test-token")
	req.Header.Set("Content-Type", "application/json")
	res := httptest.NewRecorder()

	s.Handler().ServeHTTP(res, req)

	if res.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d; body=%s", res.Code, http.StatusBadRequest, res.Body.String())
	}
}
