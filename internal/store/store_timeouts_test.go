package store

import (
	"database/sql"
	"testing"
)

// A database created before the transport and timeout columns existed must
// open cleanly, keep its devices, and read their timeouts as 0 (defaults).
func TestOpenUpgradesOldDeviceSchema(t *testing.T) {
	path := t.TempDir() + "/old.db"
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		`CREATE TABLE credentials (
		    id INTEGER PRIMARY KEY AUTOINCREMENT, name TEXT NOT NULL UNIQUE,
		    username TEXT NOT NULL, password TEXT NOT NULL DEFAULT '',
		    private_key TEXT NOT NULL DEFAULT '', enable TEXT NOT NULL DEFAULT '')`,
		`CREATE TABLE devices (
		    id INTEGER PRIMARY KEY AUTOINCREMENT, name TEXT NOT NULL UNIQUE,
		    host TEXT NOT NULL, port INTEGER NOT NULL DEFAULT 22,
		    driver TEXT NOT NULL DEFAULT 'generic',
		    credential_id INTEGER NOT NULL REFERENCES credentials(id),
		    "group" TEXT NOT NULL DEFAULT '', enabled INTEGER NOT NULL DEFAULT 1)`,
		`INSERT INTO credentials (name, username) VALUES ('c', 'u')`,
		`INSERT INTO devices (name, host, driver, credential_id) VALUES ('sw1', '10.0.0.1', 'cisco_nxos', 1)`,
	} {
		if _, err := db.Exec(q); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	db.Close()

	st, err := Open(path)
	if err != nil {
		t.Fatalf("open old schema: %v", err)
	}
	defer st.Close()
	d, err := st.GetDevice("sw1")
	if err != nil {
		t.Fatalf("GetDevice after upgrade: %v", err)
	}
	if d.Host != "10.0.0.1" || d.Driver != "cisco_nxos" || d.Transport != "" || d.CmdTimeout != 0 || d.IdleTimeout != 0 {
		t.Fatalf("upgraded device = %+v", d)
	}
	d.CmdTimeout, d.IdleTimeout = 120, 2000
	if _, err := st.CreateDevice(d); err != nil {
		t.Fatalf("CreateDevice after upgrade: %v", err)
	}
	if d, _ = st.GetDevice("sw1"); d.CmdTimeout != 120 || d.IdleTimeout != 2000 {
		t.Fatalf("timeouts not stored after upgrade: %+v", d)
	}
	devs, err := st.ListDevices()
	if err != nil || len(devs) != 1 || devs[0].CmdTimeout != 120 {
		t.Fatalf("ListDevices = %+v, %v", devs, err)
	}
}

// Re-registering a device with KeepTimeout leaves its timeouts alone; explicit
// values (0 included) replace them.
func TestCreateDeviceKeepTimeout(t *testing.T) {
	st, err := Open(t.TempDir() + "/r.db")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	cid, err := st.CreateCredential(&Credential{Name: "c", Username: "u", Password: "p"})
	if err != nil {
		t.Fatal(err)
	}
	get := func() *Device {
		t.Helper()
		d, err := st.GetDevice("r1")
		if err != nil {
			t.Fatal(err)
		}
		return d
	}
	put := func(cmd, idle int, host string) {
		t.Helper()
		d := &Device{Name: "r1", Host: host, CredentialID: cid, Enabled: true, CmdTimeout: cmd, IdleTimeout: idle}
		if _, err := st.CreateDevice(d); err != nil {
			t.Fatal(err)
		}
	}

	put(KeepTimeout, KeepTimeout, "a")
	if d := get(); d.CmdTimeout != 0 || d.IdleTimeout != 0 {
		t.Fatalf("new device with KeepTimeout = %d/%d, want 0/0", d.CmdTimeout, d.IdleTimeout)
	}
	put(90, 1500, "a")
	put(KeepTimeout, KeepTimeout, "b")
	if d := get(); d.Host != "b" || d.CmdTimeout != 90 || d.IdleTimeout != 1500 {
		t.Fatalf("after keep: host=%q timeouts=%d/%d, want b 90/1500", d.Host, d.CmdTimeout, d.IdleTimeout)
	}
	put(KeepTimeout, 0, "b")
	if d := get(); d.CmdTimeout != 90 || d.IdleTimeout != 0 {
		t.Fatalf("after partial reset: %d/%d, want 90/0", d.CmdTimeout, d.IdleTimeout)
	}
}

// UpdateDevice (used when a re-POST moves a device to another group) follows
// the same rule: explicit timeouts are written, KeepTimeout leaves them.
func TestUpdateDeviceTimeouts(t *testing.T) {
	st, err := Open(t.TempDir() + "/r.db")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	cid, err := st.CreateCredential(&Credential{Name: "c", Username: "u", Password: "p"})
	if err != nil {
		t.Fatal(err)
	}
	d := &Device{Name: "r1", Host: "a", Port: 22, CredentialID: cid, Enabled: true, CmdTimeout: 90, IdleTimeout: 1500}
	if d.ID, err = st.CreateDevice(d); err != nil {
		t.Fatal(err)
	}

	d.Group, d.CmdTimeout, d.IdleTimeout = "site", KeepTimeout, 2000
	if err := st.UpdateDevice(d, nil); err != nil {
		t.Fatal(err)
	}
	got, err := st.GetDevice("r1")
	if err != nil {
		t.Fatal(err)
	}
	if got.Group != "site" || got.CmdTimeout != 90 || got.IdleTimeout != 2000 {
		t.Fatalf("after update: group %q, timeouts %d/%d; want site, 90/2000", got.Group, got.CmdTimeout, got.IdleTimeout)
	}
}
