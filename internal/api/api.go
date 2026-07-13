// Package api exposes rusted over HTTP so external systems — notably the
// LibreNMS module shipped in ./librenms-module — can manage devices, inspect
// backup history, and trigger backups.
//
// Authentication is a static bearer token supplied via the RUSTED_API_TOKEN
// environment variable (or --token). If no token is configured the server
// refuses to start, to avoid accidentally exposing credentials management
// unauthenticated.
package api

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/athenanetworks/rusted/internal/backup"
	"github.com/athenanetworks/rusted/internal/driver"
	"github.com/athenanetworks/rusted/internal/gitstore"
	"github.com/athenanetworks/rusted/internal/store"
)

// Server holds dependencies for the HTTP API.
type Server struct {
	Store  *store.Store
	Git    *gitstore.Store
	Engine *backup.Engine
	Token  string
}

// Handler returns the configured HTTP handler (router + auth middleware).
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	mux.HandleFunc("GET /api/drivers", s.listDrivers)
	mux.HandleFunc("GET /api/credentials", s.listCredentials)
	mux.HandleFunc("POST /api/credentials", s.createCredential)
	mux.HandleFunc("DELETE /api/credentials/{name}", s.deleteCredential)
	mux.HandleFunc("GET /api/devices", s.listDevices)
	mux.HandleFunc("POST /api/devices", s.createDevice)
	mux.HandleFunc("GET /api/devices/{name}", s.getDevice)
	mux.HandleFunc("DELETE /api/devices/{name}", s.deleteDevice)
	mux.HandleFunc("GET /api/devices/{name}/history", s.deviceHistory)
	mux.HandleFunc("GET /api/devices/{name}/config", s.deviceConfig)
	mux.HandleFunc("POST /api/devices/{name}/backup", s.triggerBackup)
	return s.auth(mux)
}

func (s *Server) auth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/healthz" {
			next.ServeHTTP(w, r)
			return
		}
		hdr := r.Header.Get("Authorization")
		tok := strings.TrimPrefix(hdr, "Bearer ")
		if subtle.ConstantTimeCompare([]byte(tok), []byte(s.Token)) != 1 {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) listDrivers(w http.ResponseWriter, r *http.Request) {
	type drvDTO struct {
		Name        string `json:"name"`
		Description string `json:"description"`
	}
	var out []drvDTO
	for _, d := range driver.List() {
		out = append(out, drvDTO{d.Name, d.Description})
	}
	writeJSON(w, http.StatusOK, out)
}

// credentialDTO never exposes secret material — only whether it is set.
type credentialDTO struct {
	Name        string `json:"name"`
	Username    string `json:"username"`
	HasPassword bool   `json:"has_password"`
	HasKey      bool   `json:"has_key"`
	HasEnable   bool   `json:"has_enable"`
}

func (s *Server) listCredentials(w http.ResponseWriter, r *http.Request) {
	creds, err := s.Store.ListCredentials()
	if err != nil {
		writeErr(w, err)
		return
	}
	out := make([]credentialDTO, 0, len(creds))
	for _, c := range creds {
		out = append(out, credentialDTO{
			Name: c.Name, Username: c.Username,
			HasPassword: c.Password != "", HasKey: c.PrivateKey != "", HasEnable: c.Enable != "",
		})
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) createCredential(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Name       string `json:"name"`
		Username   string `json:"username"`
		Password   string `json:"password"`
		Enable     string `json:"enable"`
		PrivateKey string `json:"private_key"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON"})
		return
	}
	if in.Name == "" || in.Username == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "name and username are required"})
		return
	}
	if _, err := s.Store.CreateCredential(&store.Credential{
		Name: in.Name, Username: in.Username, Password: in.Password, Enable: in.Enable, PrivateKey: in.PrivateKey,
	}); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{"status": "created", "name": in.Name})
}

func (s *Server) deleteCredential(w http.ResponseWriter, r *http.Request) {
	if err := s.Store.DeleteCredential(r.PathValue("name")); err != nil {
		// DeleteCredential returns a descriptive error when still in use.
		if errors.Is(err, store.ErrNotFound) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
			return
		}
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

type deviceDTO struct {
	Name       string `json:"name"`
	Host       string `json:"host"`
	Port       int    `json:"port"`
	Driver     string `json:"driver"`
	Transport  string `json:"transport"`
	Credential string `json:"credential"`
	Group      string `json:"group"`
	Enabled    bool   `json:"enabled"`
}

func (s *Server) listDevices(w http.ResponseWriter, r *http.Request) {
	devs, err := s.Store.ListDevices()
	if err != nil {
		writeErr(w, err)
		return
	}
	out := make([]deviceDTO, 0, len(devs))
	for _, d := range devs {
		out = append(out, deviceDTO{d.Name, d.Host, d.Port, d.Driver, d.Transport, "", d.Group, d.Enabled})
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) getDevice(w http.ResponseWriter, r *http.Request) {
	d, err := s.Store.GetDevice(r.PathValue("name"))
	if err != nil {
		writeErr(w, err)
		return
	}
	dto := deviceDTO{d.Name, d.Host, d.Port, d.Driver, d.Transport, "", d.Group, d.Enabled}
	if d.Credential != nil {
		dto.Credential = d.Credential.Name
	}
	writeJSON(w, http.StatusOK, dto)
}

func (s *Server) createDevice(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Name       string `json:"name"`
		Host       string `json:"host"`
		Port       int    `json:"port"`
		Driver     string `json:"driver"`
		Transport  string `json:"transport"`
		Credential string `json:"credential"`
		Group      string `json:"group"`
		Enabled    *bool  `json:"enabled"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON"})
		return
	}
	if in.Name == "" || in.Host == "" || in.Credential == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "name, host and credential are required"})
		return
	}
	cred, err := s.Store.GetCredential(in.Credential)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown credential: " + in.Credential})
		return
	}
	enabled := true
	if in.Enabled != nil {
		enabled = *in.Enabled
	}
	d := &store.Device{
		Name: in.Name, Host: in.Host, Port: in.Port, Driver: in.Driver, Transport: in.Transport,
		CredentialID: cred.ID, Group: in.Group, Enabled: enabled,
	}
	if _, err := s.Store.CreateDevice(d); err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{"status": "created", "name": in.Name})
}

func (s *Server) deleteDevice(w http.ResponseWriter, r *http.Request) {
	if err := s.Store.DeleteDevice(r.PathValue("name")); err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

func (s *Server) deviceHistory(w http.ResponseWriter, r *http.Request) {
	runs, err := s.Store.History(r.PathValue("name"), 50)
	if err != nil {
		writeErr(w, err)
		return
	}
	type runDTO struct {
		StartedAt  time.Time `json:"started_at"`
		FinishedAt time.Time `json:"finished_at"`
		Status     string    `json:"status"`
		Message    string    `json:"message"`
		Bytes      int       `json:"bytes"`
		Commit     string    `json:"commit"`
	}
	out := make([]runDTO, 0, len(runs))
	for _, r := range runs {
		out = append(out, runDTO{r.StartedAt, r.FinishedAt, r.Status, r.Message, r.Bytes, r.Commit})
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) deviceConfig(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	d, err := s.Store.GetDevice(name)
	if err != nil {
		writeErr(w, err)
		return
	}
	rel := d.Name + ".cfg"
	if d.Group != "" {
		rel = d.Group + "/" + rel
	}
	cfg, err := s.Git.Latest(rel)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "no backup yet"})
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(cfg))
}

func (s *Server) triggerBackup(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Minute)
	defer cancel()
	res, err := s.Engine.BackupDevice(ctx, name)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, err error) {
	if errors.Is(err, store.ErrNotFound) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
		return
	}
	writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
}
