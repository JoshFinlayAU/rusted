package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/athenanetworks/rusted/internal/gitstore"
	"github.com/athenanetworks/rusted/internal/store"
)

func newTestServer(t *testing.T) *Server {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	gs, err := gitstore.Open(filepath.Join(t.TempDir(), "backups"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreateCredential(&store.Credential{Name: "c", Username: "u", Password: "p"}); err != nil {
		t.Fatal(err)
	}
	return &Server{Store: st, Git: gs, Token: "test-token"}
}

func call(t *testing.T, s *Server, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer test-token")
	res := httptest.NewRecorder()
	s.Handler().ServeHTTP(res, req)
	return res
}

func TestListTransports(t *testing.T) {
	res := call(t, newTestServer(t), "GET", "/api/transports", "")
	var names []string
	if err := json.Unmarshal(res.Body.Bytes(), &names); err != nil || !slices.Contains(names, "ssh") {
		t.Fatalf("transports = %s (%v)", res.Body, err)
	}
}

func TestCreateDeviceRejectsUnsafeNames(t *testing.T) {
	s := newTestServer(t)
	for _, body := range []string{
		`{"name":"site/core1","host":"h","credential":"c"}`,
		`{"name":"../x","host":"h","credential":"c"}`,
		`{"name":"r1","host":"h","credential":"c","group":"/etc"}`,
		`{"name":"r1","host":"h","credential":"c","group":"a/../../b"}`,
	} {
		if res := call(t, s, "POST", "/api/devices", body); res.Code != http.StatusBadRequest {
			t.Errorf("%s: status %d, want 400; %s", body, res.Code, res.Body)
		}
	}
}

func TestDeviceListStatusAndVersions(t *testing.T) {
	s := newTestServer(t)
	if res := call(t, s, "POST", "/api/devices", `{"name":"r1","host":"h","credential":"c","group":"dc"}`); res.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", res.Code, res.Body)
	}
	d, _ := s.Store.GetDevice("r1")
	sr, err := s.Git.Save("dc/r1.cfg", "hostname r1\n", "backup r1")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	if err := s.Store.RecordRun(&store.BackupRun{DeviceID: d.ID, StartedAt: now, FinishedAt: now, Status: "success", Message: "configuration updated", Commit: sr.Commit}); err != nil {
		t.Fatal(err)
	}

	var list []map[string]any
	if err := json.Unmarshal(call(t, s, "GET", "/api/devices", "").Body.Bytes(), &list); err != nil || len(list) != 1 {
		t.Fatalf("list: %v %v", list, err)
	}
	for _, k := range []string{"last_backup", "last_status", "last_message", "enabled", "credential"} {
		if _, ok := list[0][k]; !ok {
			t.Errorf("device list lacks %q: %v", k, list[0])
		}
	}
	if list[0]["last_status"] != "success" || list[0]["credential"] != "c" {
		t.Errorf("unexpected list entry %v", list[0])
	}

	var versions []map[string]string
	if err := json.Unmarshal(call(t, s, "GET", "/api/devices/r1/versions", "").Body.Bytes(), &versions); err != nil || len(versions) != 1 {
		t.Fatalf("versions: %v %v", versions, err)
	}
	if v := versions[0]; v["author"] != "rusted" || v["captured_at"] == "" || len(v["date"]) != 10 || !strings.HasPrefix(sr.Commit, v["commit"]) {
		t.Errorf("unexpected version %v", v)
	}
	if res := call(t, s, "GET", "/api/devices/r1/versions?limit=5000", ""); res.Code != http.StatusBadRequest {
		t.Errorf("limit above cap: status %d", res.Code)
	}
}

// Re-registering a device under another group moves its file, and deleting it
// removes the file from the repository in a commit.
func TestDeviceGroupMoveAndDelete(t *testing.T) {
	s := newTestServer(t)
	call(t, s, "POST", "/api/devices", `{"name":"r1","host":"h","credential":"c","group":"dc"}`)
	call(t, s, "POST", "/api/devices", `{"name":"r2","host":"h","credential":"c","group":"dc"}`)
	if _, err := s.Git.Save("dc/r1.cfg", "hostname r1\n", "backup r1"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Git.Save("dc/r2.cfg", "hostname r2\n", "backup r2"); err != nil {
		t.Fatal(err)
	}

	if res := call(t, s, "POST", "/api/devices", `{"name":"r1","host":"h","credential":"c","group":"edge"}`); res.Code != http.StatusCreated {
		t.Fatalf("re-register: %d %s", res.Code, res.Body)
	}
	if got, err := s.Git.Latest("edge/r1.cfg"); err != nil || got != "hostname r1\n" {
		t.Fatalf("file not moved with the group: %q %v", got, err)
	}

	if res := call(t, s, "DELETE", "/api/devices/r1", ""); res.Code != http.StatusOK {
		t.Fatalf("delete: %d %s", res.Code, res.Body)
	}
	if _, err := os.Stat(filepath.Join(s.Git.Dir, "edge")); !os.IsNotExist(err) {
		t.Fatalf("config (and emptied group dir) should be gone, stat err = %v", err)
	}
	if got, _ := s.Git.Latest("dc/r2.cfg"); got != "hostname r2\n" {
		t.Fatalf("delete touched another device's file: %q", got)
	}
	if res := call(t, s, "DELETE", "/api/devices/r1", ""); res.Code != http.StatusNotFound {
		t.Fatalf("second delete: %d", res.Code)
	}
}
