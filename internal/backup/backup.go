// Package backup ties the store, transport, driver, and git layers together to
// back up one or many devices.
package backup

import (
	"context"
	"fmt"
	"io"
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
	Log       io.Writer     // verbose progress output (nil = silent)
	Debug     io.Writer     // raw I/O debug output (nil = silent; set by --debug/-vv)
	// Raw saves the captured configuration verbatim: the driver's Strip rules
	// are skipped and dynamic strings are not masked (--raw).
	Raw bool
}

// New builds an Engine with sensible defaults.
func New(st *store.Store, gs *gitstore.Store) *Engine {
	return &Engine{Store: st, Git: gs, Transport: "ssh", Timeout: 30 * time.Second}
}

// logf writes a verbose progress line to the engine's Log writer if set.
// It is a no-op when Log is nil (the default), so production runs stay quiet.
func (e *Engine) logf(format string, args ...any) {
	if e.Log != nil {
		fmt.Fprintf(e.Log, format, args...)
	}
}

// Result is the outcome of backing up a single device.
type Result struct {
	Device  string `json:"device"`
	Status  string `json:"status"` // "success", "unchanged", "failed"
	Message string `json:"message"`
	Commit  string `json:"commit"`
	Bytes   int    `json:"bytes"`
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
	e.logf("=== %s [%s:%s] ===\n", dev.Name, dev.Host, dev.Driver)

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

	drv, known := driver.Get(dev.Driver)
	if !known {
		// Not fatal: generic driver is used, but note it.
		res.Message = fmt.Sprintf("unknown driver %q, used generic", dev.Driver)
	}

	// Transport precedence: a device can pin its own ("routeros-api"), else the
	// driver's preferred transport (MikroTik pins "ssh-exec"), else the engine
	// default (interactive ssh).
	transportName := e.Transport
	if drv.Transport != "" {
		transportName = drv.Transport
	}
	if dev.Transport != "" {
		transportName = dev.Transport
	}
	tr, err := transport.Get(transportName)
	if err != nil {
		return finish("failed", err.Error(), "", 0)
	}
	e.logf("using transport %q, driver %q\n", transportName, drv.Name)

	tgt := transport.Target{
		Name:       dev.Name,
		Host:       dev.Host,
		Port:       dev.Port,
		Username:   dev.Credential.Username,
		Password:   dev.Credential.Password,
		PrivateKey: []byte(dev.Credential.PrivateKey),
		Enable:     dev.Credential.Enable,
		Timeout:    e.Timeout,
		Debug:      e.Debug,
		RawOutput:  drv.BinarySafe, // byte-exact capture for post-processing drivers
	}
	if dev.CmdTimeout > 0 {
		tgt.CmdTimeout = time.Duration(dev.CmdTimeout) * time.Second
	}
	if dev.IdleTimeout > 0 {
		tgt.IdleTimeout = time.Duration(dev.IdleTimeout) * time.Millisecond
	}

	dialCtx, cancel := context.WithTimeout(ctx, e.Timeout)
	defer cancel()
	sess, err := tr.Dial(dialCtx, tgt)
	if err != nil {
		e.logf("  connect FAILED: %v\n", err)
		return finish("failed", "connect: "+err.Error(), "", 0)
	}
	defer sess.Close()
	e.logf("  connected, running %d init command(s)\n", len(drv.Init))

	for _, c := range drv.Init {
		if _, err := sess.SendCommand(c); err != nil {
			e.logf("  init %q FAILED: %v\n", c, err)
			return finish("failed", fmt.Sprintf("init command %q: %v", c, err), "", 0)
		}
	}
	e.logf("  init commands ok, collecting config (%d command(s))\n", len(drv.Config))

	var buf strings.Builder
	for _, c := range drv.Config {
		out, err := sess.SendCommand(c)
		if err != nil {
			e.logf("  config %q FAILED: %v\n", c, err)
			return finish("failed", fmt.Sprintf("config command %q: %v", c, err), "", 0)
		}
		buf.WriteString(out)
		// Framing newline between commands — but never for BinarySafe
		// drivers, where a missing trailing newline may be part of the file
		// content and post-processing needs the bytes untouched.
		if !drv.BinarySafe && !strings.HasSuffix(out, "\n") {
			buf.WriteByte('\n')
		}
	}

	raw := buf.String()
	if drv.PostProcess != nil {
		raw = drv.PostProcess(raw)
	}
	var clean string
	if e.Raw {
		e.logf("  raw mode: skipping volatile-line stripping and masking\n")
		clean = drv.CleanRaw(raw)
	} else {
		clean = drv.Clean(raw)
	}
	if strings.TrimSpace(clean) == "" {
		e.logf("  captured empty configuration\n")
		return finish("failed", "captured empty configuration", "", 0)
	}
	e.logf("  captured %d bytes, saving to git...\n", len(clean))

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
