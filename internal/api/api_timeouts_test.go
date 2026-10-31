package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/athenanetworks/rusted/internal/store"
)

func callTimeouts(s *Server, method, path, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer test-token")
	req.Header.Set("Content-Type", "application/json")
	res := httptest.NewRecorder()
	s.Handler().ServeHTTP(res, req)
	return res
}

// cmd_timeout / idle_timeout round-trip through create and get, and a
// re-POST that omits them (as the LibreNMS plugin does on every edit) keeps
// the stored values instead of resetting them to 0.
func TestDeviceTimeouts(t *testing.T) {
	st, err := store.Open(t.TempDir() + "/r.db")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if _, err := st.CreateCredential(&store.Credential{Name: "c", Username: "u", Password: "p"}); err != nil {
		t.Fatal(err)
	}
	s := &Server{Store: st, Token: "test-token"}

	get := func() deviceDTO {
		t.Helper()
		res := callTimeouts(s, http.MethodGet, "/api/devices/sw1", "")
		if res.Code != http.StatusOK {
			t.Fatalf("get: %d %s", res.Code, res.Body.String())
		}
		var d deviceDTO
		if err := json.Unmarshal(res.Body.Bytes(), &d); err != nil {
			t.Fatal(err)
		}
		return d
	}
	post := func(body string, want int) {
		t.Helper()
		if res := callTimeouts(s, http.MethodPost, "/api/devices", body); res.Code != want {
			t.Fatalf("POST %s: %d %s, want %d", body, res.Code, res.Body.String(), want)
		}
	}

	post(`{"name":"sw1","host":"10.0.0.1","credential":"c","cmd_timeout":120,"idle_timeout":2500}`, http.StatusCreated)
	if d := get(); d.CmdTimeout != 120 || d.IdleTimeout != 2500 {
		t.Fatalf("after create: %+v", d)
	}
	post(`{"name":"sw1","host":"10.0.0.2","credential":"c"}`, http.StatusCreated)
	if d := get(); d.Host != "10.0.0.2" || d.CmdTimeout != 120 || d.IdleTimeout != 2500 {
		t.Fatalf("re-POST without timeouts reset them: %+v", d)
	}
	post(`{"name":"sw1","host":"10.0.0.2","credential":"c","cmd_timeout":0}`, http.StatusCreated)
	if d := get(); d.CmdTimeout != 0 || d.IdleTimeout != 2500 {
		t.Fatalf("explicit 0 not applied: %+v", d)
	}
	post(`{"name":"sw1","host":"10.0.0.2","credential":"c","idle_timeout":-5}`, http.StatusBadRequest)

	res := callTimeouts(s, http.MethodGet, "/api/devices", "")
	if !strings.Contains(res.Body.String(), `"idle_timeout":2500`) {
		t.Fatalf("list does not report timeouts: %s", res.Body.String())
	}
}
