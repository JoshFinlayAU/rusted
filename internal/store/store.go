// Package store is the SQLite-backed persistence layer for rusted. It holds
// credentials, devices, and the history of backup runs.
package store

import (
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/athenanetworks/rusted/internal/secret"
	_ "modernc.org/sqlite"
)

// ErrNotFound is returned when a lookup by name or id matches no row.
var ErrNotFound = errors.New("not found")

// Store wraps the SQLite database handle.
type Store struct {
	db *sql.DB
}

// Credential is a reusable set of login secrets referenced by devices.
type Credential struct {
	ID         int64
	Name       string
	Username   string
	Password   string // decrypted in memory
	PrivateKey string // decrypted in memory; PEM data, optional
	Enable     string // decrypted in memory; enable/privileged password, optional
}

// Device is a network device to be backed up.
type Device struct {
	ID           int64
	Name         string
	Host         string
	Port         int
	Driver       string
	Transport    string // transport name (e.g. "routeros-api"); "" = engine default (ssh)
	CredentialID int64
	Group        string // sub-directory within the backup repo
	Enabled      bool
	// Credential is populated by lookups that join (may be nil).
	Credential *Credential
}

// BackupRun records the outcome of a single backup attempt.
type BackupRun struct {
	ID         int64
	DeviceID   int64
	DeviceName string
	StartedAt  time.Time
	FinishedAt time.Time
	Status     string // "success", "unchanged", "failed"
	Message    string
	Bytes      int
	Commit     string
}

// Open opens (creating if necessary) the SQLite database at path and applies
// the schema.
func Open(path string) (*Store, error) {
	db, err := sql.Open("sqlite", path+"?_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)&_pragma=journal_mode(WAL)")
	if err != nil {
		return nil, err
	}
	s := &Store{db: db}
	if err := s.migrate(); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

// Close closes the underlying database.
func (s *Store) Close() error { return s.db.Close() }

func (s *Store) migrate() error {
	const schema = `
CREATE TABLE IF NOT EXISTS credentials (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    name        TEXT NOT NULL UNIQUE,
    username    TEXT NOT NULL,
    password    TEXT NOT NULL DEFAULT '',
    private_key TEXT NOT NULL DEFAULT '',
    enable      TEXT NOT NULL DEFAULT ''
);
CREATE TABLE IF NOT EXISTS devices (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    name          TEXT NOT NULL UNIQUE,
    host          TEXT NOT NULL,
    port          INTEGER NOT NULL DEFAULT 22,
    driver        TEXT NOT NULL DEFAULT 'generic',
    transport     TEXT NOT NULL DEFAULT '',
    credential_id INTEGER NOT NULL REFERENCES credentials(id),
    "group"       TEXT NOT NULL DEFAULT '',
    enabled       INTEGER NOT NULL DEFAULT 1
);
CREATE TABLE IF NOT EXISTS backup_runs (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    device_id   INTEGER NOT NULL REFERENCES devices(id) ON DELETE CASCADE,
    started_at  TIMESTAMP NOT NULL,
    finished_at TIMESTAMP NOT NULL,
    status      TEXT NOT NULL,
    message     TEXT NOT NULL DEFAULT '',
    bytes       INTEGER NOT NULL DEFAULT 0,
    commit_hash TEXT NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS idx_runs_device ON backup_runs(device_id, started_at DESC);
`
	if _, err := s.db.Exec(schema); err != nil {
		return err
	}
	// Additive migrations for databases created before a column existed
	// (CREATE TABLE IF NOT EXISTS never adds columns to an existing table).
	s.addColumnIfMissing("devices", "transport", "TEXT NOT NULL DEFAULT ''")
	return nil
}

// addColumnIfMissing ALTERs a table to add a column when it isn't already present,
// so upgrades don't need a full rebuild. Best-effort: a failure here just means an
// older schema, surfaced by the next query rather than a crash on boot.
func (s *Store) addColumnIfMissing(table, column, def string) {
	rows, err := s.db.Query(`SELECT 1 FROM pragma_table_info(?) WHERE name = ?`, table, column)
	if err != nil {
		return
	}
	present := rows.Next()
	rows.Close()
	if present {
		return
	}
	_, _ = s.db.Exec(`ALTER TABLE ` + table + ` ADD COLUMN ` + column + ` ` + def)
}

// --- Credentials ---

// CreateCredential inserts a new credential, sealing its secret fields.
func (s *Store) CreateCredential(c *Credential) (int64, error) {
	pw, err := secret.Seal(c.Password)
	if err != nil {
		return 0, err
	}
	pk, err := secret.Seal(c.PrivateKey)
	if err != nil {
		return 0, err
	}
	en, err := secret.Seal(c.Enable)
	if err != nil {
		return 0, err
	}
	res, err := s.db.Exec(
		`INSERT INTO credentials (name, username, password, private_key, enable) VALUES (?,?,?,?,?)`,
		c.Name, c.Username, pw, pk, en)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func scanCredential(row interface{ Scan(...any) error }) (*Credential, error) {
	var c Credential
	var pw, pk, en string
	if err := row.Scan(&c.ID, &c.Name, &c.Username, &pw, &pk, &en); err != nil {
		return nil, err
	}
	var err error
	if c.Password, err = secret.Open(pw); err != nil {
		return nil, err
	}
	if c.PrivateKey, err = secret.Open(pk); err != nil {
		return nil, err
	}
	if c.Enable, err = secret.Open(en); err != nil {
		return nil, err
	}
	return &c, nil
}

// GetCredential looks up a credential by name.
func (s *Store) GetCredential(name string) (*Credential, error) {
	row := s.db.QueryRow(`SELECT id, name, username, password, private_key, enable FROM credentials WHERE name = ?`, name)
	c, err := scanCredential(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return c, err
}

// ListCredentials returns all credentials ordered by name.
func (s *Store) ListCredentials() ([]*Credential, error) {
	rows, err := s.db.Query(`SELECT id, name, username, password, private_key, enable FROM credentials ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Credential
	for rows.Next() {
		c, err := scanCredential(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// DeleteCredential removes a credential by name. It fails if devices still
// reference it.
func (s *Store) DeleteCredential(name string) error {
	c, err := s.GetCredential(name)
	if err != nil {
		return err
	}
	var n int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM devices WHERE credential_id = ?`, c.ID).Scan(&n); err != nil {
		return err
	}
	if n > 0 {
		return fmt.Errorf("credential %q is still used by %d device(s)", name, n)
	}
	_, err = s.db.Exec(`DELETE FROM credentials WHERE id = ?`, c.ID)
	return err
}

// --- Devices ---

// CreateDevice inserts a new device. CredentialID must reference an existing
// credential.
func (s *Store) CreateDevice(d *Device) (int64, error) {
	if d.Port == 0 {
		d.Port = 22
	}
	if d.Driver == "" {
		d.Driver = "generic"
	}
	// Upsert on the unique name so a re-register (the caller syncs state before every
	// backup) updates host/driver/transport/credential/enabled instead of failing.
	res, err := s.db.Exec(
		`INSERT INTO devices (name, host, port, driver, transport, credential_id, "group", enabled)
		 VALUES (?,?,?,?,?,?,?,?)
		 ON CONFLICT(name) DO UPDATE SET
		   host=excluded.host, port=excluded.port, driver=excluded.driver,
		   transport=excluded.transport, credential_id=excluded.credential_id,
		   "group"=excluded."group", enabled=excluded.enabled`,
		d.Name, d.Host, d.Port, d.Driver, d.Transport, d.CredentialID, d.Group, d.Enabled)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

const deviceCols = `d.id, d.name, d.host, d.port, d.driver, d.transport, d.credential_id, d."group", d.enabled`

func scanDevice(row interface{ Scan(...any) error }) (*Device, error) {
	var d Device
	if err := row.Scan(&d.ID, &d.Name, &d.Host, &d.Port, &d.Driver, &d.Transport, &d.CredentialID, &d.Group, &d.Enabled); err != nil {
		return nil, err
	}
	return &d, nil
}

// GetDevice looks up a device by name, populating its Credential.
func (s *Store) GetDevice(name string) (*Device, error) {
	row := s.db.QueryRow(`SELECT `+deviceCols+` FROM devices d WHERE d.name = ?`, name)
	d, err := scanDevice(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	d.Credential, err = s.getCredentialByID(d.CredentialID)
	return d, err
}

func (s *Store) getCredentialByID(id int64) (*Credential, error) {
	row := s.db.QueryRow(`SELECT id, name, username, password, private_key, enable FROM credentials WHERE id = ?`, id)
	c, err := scanCredential(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return c, err
}

// ListDevices returns all devices ordered by name. Credentials are not
// populated.
func (s *Store) ListDevices() ([]*Device, error) {
	rows, err := s.db.Query(`SELECT ` + deviceCols + ` FROM devices d ORDER BY d.name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Device
	for rows.Next() {
		d, err := scanDevice(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// SetDeviceEnabled toggles a device's enabled flag.
func (s *Store) SetDeviceEnabled(name string, enabled bool) error {
	res, err := s.db.Exec(`UPDATE devices SET enabled = ? WHERE name = ?`, enabled, name)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// DeleteDevice removes a device and its backup history.
func (s *Store) DeleteDevice(name string) error {
	res, err := s.db.Exec(`DELETE FROM devices WHERE name = ?`, name)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// --- Backup runs ---

// RecordRun stores the outcome of a backup attempt.
func (s *Store) RecordRun(r *BackupRun) error {
	_, err := s.db.Exec(
		`INSERT INTO backup_runs (device_id, started_at, finished_at, status, message, bytes, commit_hash) VALUES (?,?,?,?,?,?,?)`,
		r.DeviceID, r.StartedAt, r.FinishedAt, r.Status, r.Message, r.Bytes, r.Commit)
	return err
}

// History returns backup runs for a device (most recent first), limited to
// limit rows (0 = no limit).
func (s *Store) History(deviceName string, limit int) ([]*BackupRun, error) {
	q := `SELECT r.id, r.device_id, d.name, r.started_at, r.finished_at, r.status, r.message, r.bytes, r.commit_hash
	      FROM backup_runs r JOIN devices d ON d.id = r.device_id
	      WHERE d.name = ? ORDER BY r.started_at DESC`
	if limit > 0 {
		q += fmt.Sprintf(" LIMIT %d", limit)
	}
	rows, err := s.db.Query(q, deviceName)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*BackupRun
	for rows.Next() {
		var r BackupRun
		if err := rows.Scan(&r.ID, &r.DeviceID, &r.DeviceName, &r.StartedAt, &r.FinishedAt, &r.Status, &r.Message, &r.Bytes, &r.Commit); err != nil {
			return nil, err
		}
		out = append(out, &r)
	}
	return out, rows.Err()
}
