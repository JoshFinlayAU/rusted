# rusted

A network device configuration backup tool — a modern, single-binary
replacement for **RANCID** and **Oxidized**.

rusted connects to your network devices over SSH, captures their running
configuration using per-platform drivers, masks volatile data so backups are
diff-stable, and versions every change in a git repository. Credentials and
device inventory live in a local SQLite database, and an HTTP API drives the
bundled **LibreNMS** integration.

## Features

- **SSH transport**, with a documented, pluggable transport interface
  (telnet/NETCONF/REST can be added without touching the core) — see
  [docs/transport-modules.md](docs/transport-modules.md).
- **Per-platform drivers** that know how to disable paging and dump config —
  see [docs/drivers.md](docs/drivers.md).
- **Change-stable backups**: volatile lines are stripped *and* inline
  timestamps/dates/uptimes are masked, so an unchanged device never produces a
  spurious commit.
- **Git-versioned storage** under `./backups`, one file per device.
- **SQLite-backed** credential and device store, with optional
  **encryption-at-rest** for secrets.
- **HTTP API** + **LibreNMS module** for add/remove device, history, and
  on-demand backups.
- **No Python, no runtime dependencies** beyond `git` — ships as one Go binary.

## Supported platforms

Officially supported (per the project spec):

- **Cisco Nexus (NX-OS)** — `cisco_nxos`
- **MikroTik RouterOS v7+** — `mikrotik_routeros`
- **Juniper Junos** — `juniper_junos`

Also bundled: `cisco_ios`, `cisco_asa`, `arista_eos`, `fortinet`, `vyos`,
`generic`. Run `rusted driver list` to see them all.

## Build

```sh
go build -o rusted ./cmd/rusted
```

Requires Go 1.26+ and the `git` binary on `PATH`.

## Quick start

```sh
# Initialise the database and the ./backups git repo
./rusted init

# Encrypt stored credentials at rest (recommended): keep this secret safe and
# stable — losing it makes encrypted credentials unrecoverable.
export RUSTED_SECRET="a-long-random-passphrase"

# Add a reusable credential
./rusted cred add lab -u admin -p 's3cret' -e 'enablepw'
#   -k ./id_ed25519   # optionally use a private key instead of/with a password

# Add devices (driver = platform; group = sub-directory in the backup repo)
./rusted device add nexus1  -H 10.0.0.1 -d cisco_nxos        -c lab -g datacenter
./rusted device add edge-mt -H 10.0.0.2 -d mikrotik_routeros -c lab
./rusted device add core-mx -H 10.0.0.3 -d juniper_junos     -c lab

# Back up one device, or everything enabled
./rusted backup run nexus1
./rusted backup run --all

# Inspect results
./rusted backup history nexus1
git -C backups log --oneline
```

## Command reference

| Command | Purpose |
|---|---|
| `rusted init` | Create the DB and backup repo |
| `rusted cred add/list/remove` | Manage login credentials |
| `rusted device add/list/remove/enable/disable` | Manage device inventory |
| `rusted driver list` | List platform drivers |
| `rusted backup run [NAME] [--all]` | Run backups |
| `rusted backup history NAME` | Show a device's backup history |
| `rusted serve` | Run the HTTP API for LibreNMS |

Global flags: `--db` (default `rusted.db`, or `$RUSTED_DB`) and `--backups`
(default `backups`, or `$RUSTED_BACKUPS`).

## Credential encryption

If `RUSTED_SECRET` is set, password / private-key / enable fields are encrypted
with AES-256-GCM before being written to SQLite (values are prefixed `enc:`).
If it is unset, secrets are stored in plaintext and rusted warns you. Plaintext
and encrypted rows can coexist, so you can enable encryption later — but rows
written while encrypted require the same `RUSTED_SECRET` to read.

## HTTP API / LibreNMS

```sh
export RUSTED_API_TOKEN="a-long-random-token"
./rusted serve --addr :8080
```

All `/api/*` routes require `Authorization: Bearer $RUSTED_API_TOKEN`
(`/healthz` is open). Endpoints:

| Method & path | Description |
|---|---|
| `GET /api/devices` | List devices |
| `POST /api/devices` | Add a device (JSON) |
| `GET /api/devices/{name}` | Device detail |
| `DELETE /api/devices/{name}` | Remove a device |
| `GET /api/devices/{name}/history` | Backup history |
| `GET /api/devices/{name}/config` | Latest stored config (text) |
| `POST /api/devices/{name}/backup` | Trigger a backup now |
| `GET /api/drivers` | List drivers |

The LibreNMS plugin that consumes this API lives in
[`librenms-module/`](librenms-module/README.md).

## How change detection works

For each backup, rusted runs the driver's config commands, applies the driver's
line `Strip` rules, masks dynamic substrings (`internal/normalize`), then writes
the result to `backups/<group>/<device>.cfg`. It commits **only if the file
content actually changed** — so timestamps, uptimes, and "last changed" banners
never create noise in your git history. A run is recorded as `success`
(committed), `unchanged` (no diff), or `failed`.

## Roadmap

- `known_hosts` host-key pinning (SSH currently accepts any host key).
- Concurrent `--all` backups with a worker pool.
- Scheduled backups (cron) and webhook/Slack notifications.
- Telnet and NETCONF transports.

## Project layout

```
cmd/rusted/         CLI (cobra)
internal/store/     SQLite: credentials, devices, run history
internal/secret/    AES-GCM encryption-at-rest
internal/transport/ transport interface + SSH implementation
internal/driver/    per-platform drivers
internal/normalize/ dynamic-string (timestamp/date/uptime) masking
internal/gitstore/  git-backed backup storage
internal/backup/    backup engine
internal/api/       HTTP API for LibreNMS
librenms-module/    LibreNMS plugin
docs/               transport & driver authoring guides
```
