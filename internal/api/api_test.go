package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func postExecute(s *Server, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/api/devices/router-1/execute", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer test-token")
	req.Header.Set("Content-Type", "application/json")
	res := httptest.NewRecorder()
	s.Handler().ServeHTTP(res, req)
	return res
}

func TestExecuteDisabledByDefault(t *testing.T) {
	res := postExecute(&Server{Token: "test-token"}, `{"command":"show version"}`)
	if res.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d; body=%s", res.Code, http.StatusForbidden, res.Body.String())
	}
}

func TestExecuteRequiresToken(t *testing.T) {
	s := &Server{Token: "test-token", AllowExec: true}
	req := httptest.NewRequest(http.MethodPost, "/api/devices/router-1/execute", strings.NewReader(`{"command":"show version"}`))
	res := httptest.NewRecorder()
	s.Handler().ServeHTTP(res, req)
	if res.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", res.Code, http.StatusUnauthorized)
	}
}

func TestExecuteValidatesInput(t *testing.T) {
	s := &Server{Token: "test-token", AllowExec: true}
	for name, body := range map[string]string{
		"empty command": `{"command":"   "}`,
		"long command":  `{"command":"` + strings.Repeat("a", 4097) + `"}`,
		"timeout low":   `{"command":"x","timeout":-1}`,
		"timeout high":  `{"command":"x","timeout":61}`,
		"bad json":      `{"command":`,
	} {
		if res := postExecute(s, body); res.Code != http.StatusBadRequest {
			t.Errorf("%s: status = %d, want %d; body=%s", name, res.Code, http.StatusBadRequest, res.Body.String())
		}
	}
}

func TestHealthzReportsVersion(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	res := httptest.NewRecorder()
	(&Server{Token: "test-token", Version: "1.2.3"}).Handler().ServeHTTP(res, req)
	if res.Code != http.StatusOK || !strings.Contains(res.Body.String(), `"version":"1.2.3"`) {
		t.Fatalf("healthz = %d %s; want 200 with version 1.2.3", res.Code, res.Body.String())
	}
}
