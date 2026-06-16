// Package backup ties the store, transport, driver, and git layers together to
// back up one or many devices.
package backup

import (
	"context"
	"fmt"
	"path"
	"strings"
	"time"

	"github.com/athenanetworks/rusted/internal/driver"
	"github.com/athenanetworks/rusted/internal/gitstore"
	"github.com/athenanetworks/rusted/internal/store"
	"github.com/athenanetworks/rusted/internal/transport"
)

// Engine performs backups using the given store and git backend.
type Engine struct {
	Store     *store.Store
	Git       *gitstore.Store
	Transport string        // transport name, default "ssh"
	Timeout   time.Duration // per-device connect timeout
}

// New builds an Engine with sensible defaults.
func New(st *store.Store, gs *gitstore.Store) *Engine {
	return &Engine{Store: st, Git: gs, Transport: "ssh", Timeout: 30 * time.Second}
}

// Result is the outcome of backing up a single device.
type Result struct {
	Device  string
	Status  string // "success", "unchanged", "failed"
	Message string
	Commit  string
	Bytes   int
}

// BackupDevice backs up one device by name and records the run.
func (e *Engine) BackupDevice(ctx context.Context, name string) (*Result, error) {
	dev, err := e.Store.GetDevice(name)
	if err != nil {
		return nil, err
	}
	return e.backup(ctx, dev), nil
}

// BackupAll backs up every enabled device, returning one Result per device.
func (e *Engine) BackupAll(ctx context.Context) ([]*Result, error) {
	devs, err := e.Store.ListDevices()
	if err != nil {
		return nil, err
	}
	var results []*Result
	for _, d := range devs {
		if !d.Enabled {
			continue
		}
		full, err := e.Store.GetDevice(d.Name) // re-fetch to populate credential
		if err != nil {
			results = append(results, &Result{Device: d.Name, Status: "failed", Message: err.Error()})
			continue
		}
		results = append(results, e.backup(ctx, full))
	}
	return results, nil
}

func (e *Engine) backup(ctx context.Context, dev *store.Device) *Result {
	started := time.Now()
	res := &Result{Device: dev.Name}

	finish := func(status, msg, commit string, n int) *Result {
		res.Status, res.Message, res.Commit, res.Bytes = status, msg, commit, n
		_ = e.Store.RecordRun(&store.BackupRun{
			DeviceID:   dev.ID,
			StartedAt:  started,
			FinishedAt: time.Now(),
			Status:     status,
			Message:    msg,
			Bytes:      n,
			Commit:     commit,
		})
		return res
	}

	if dev.Credential == nil {
		return finish("failed", "device has no credential", "", 0)
	}

	tr, err := transport.Get(e.Transport)
	if err != nil {
		return finish("failed", err.Error(), "", 0)
	}
	drv, known := driver.Get(dev.Driver)
	if !known {
		// Not fatal: generic driver is used, but note it.
		res.Message = fmt.Sprintf("unknown driver %q, used generic", dev.Driver)
	}

	tgt := transport.Target{
		Name:       dev.Name,
		Host:       dev.Host,
		Port:       dev.Port,
		Username:   dev.Credential.Username,
		Password:   dev.Credential.Password,
		PrivateKey: []byte(dev.Credential.PrivateKey),
		Enable:     dev.Credential.Enable,
		Timeout:    e.Timeout,
	}

	dialCtx, cancel := context.WithTimeout(ctx, e.Timeout)
	defer cancel()
	sess, err := tr.Dial(dialCtx, tgt)
	if err != nil {
		return finish("failed", "connect: "+err.Error(), "", 0)
	}
	defer sess.Close()

	for _, c := range drv.Init {
		if _, err := sess.SendCommand(c); err != nil {
			return finish("failed", fmt.Sprintf("init command %q: %v", c, err), "", 0)
		}
	}

	var buf strings.Builder
	for _, c := range drv.Config {
		out, err := sess.SendCommand(c)
		if err != nil {
			return finish("failed", fmt.Sprintf("config command %q: %v", c, err), "", 0)
		}
		buf.WriteString(out)
		if !strings.HasSuffix(out, "\n") {
			buf.WriteByte('\n')
		}
	}

	clean := drv.Clean(buf.String())
	if strings.TrimSpace(clean) == "" {
		return finish("failed", "captured empty configuration", "", 0)
	}

	relPath := dev.Name + ".cfg"
	if dev.Group != "" {
		relPath = path.Join(dev.Group, relPath)
	}
	msg := fmt.Sprintf("backup %s @ %s", dev.Name, started.Format(time.RFC3339))
	sr, err := e.Git.Save(relPath, clean, msg)
	if err != nil {
		return finish("failed", "git: "+err.Error(), "", len(clean))
	}
	if sr.Changed {
		return finish("success", "configuration updated", sr.Commit, sr.Bytes)
	}
	return finish("unchanged", "no configuration change", sr.Commit, sr.Bytes)
}
