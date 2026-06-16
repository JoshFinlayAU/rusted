# rusted

A network device configuration backup tool - a modern, single-binary
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

## Install

The installer builds rusted, installs the binary, generates a config file with a
random API token and encryption secret, and initialises the database and backup
repo. Requires Go 1.26+ and `git`.

```sh
./install.sh             # install for the current user (default)
./install.sh --global    # install system-wide (uses sudo)
./install.sh --global --service   # also install + enable a systemd service
```

| | User install | Global install |
|---|---|---|
| binary | `~/.local/bin/rusted` | `/usr/local/bin/rusted` |
| config | `~/.config/rusted/config.toml` | `/etc/rusted/config.toml` |
| data (db + backups) | `~/.local/share/rusted` | `/var/lib/rusted` |

(Paths honour `XDG_*` overrides.) Re-running the installer never overwrites an
existing config, so encrypted credentials stay readable across upgrades.

To build manually instead: `go build -o rusted ./cmd/rusted`.

## Configuration

Settings resolve in this order (later wins):

```
built-in defaults  <  config file  <  environment variables  <  CLI flags
```

The config file is searched at `$RUSTED_CONFIG`, then
`~/.config/rusted/config.toml`, then `/etc/rusted/config.toml` (override with
`--config`). Generate one with:

```sh
rusted config init            # user-level
rusted config init --global   # system-wide
rusted config show            # print resolved config (secrets masked)
```

It is a small TOML-style file (mode `0600` — it holds secrets):

```toml
db        = "/var/lib/rusted/rusted.db"
backups   = "/var/lib/rusted/backups"
api_addr  = ":8080"
api_token = "…"   # bearer token for the HTTP API / LibreNMS
secret    = "…"   # AES-256-GCM key for credential encryption-at-rest
```

Each key has an environment-variable equivalent: `RUSTED_DB`, `RUSTED_BACKUPS`,
`RUSTED_API_ADDR`, `RUSTED_API_TOKEN`, `RUSTED_SECRET`.

## Quick start

```sh
# (install.sh already ran 'init' and created the config)

# Add a reusable credential
rusted cred add lab -u admin -p 's3cret' -e 'enablepw'
#   -k ./id_ed25519   # optionally use a private key instead of/with a password

# Add devices (driver = platform; group = sub-directory in the backup repo)
rusted device add nexus1  -H 10.0.0.1 -d cisco_nxos        -c lab -g datacenter
rusted device add edge-mt -H 10.0.0.2 -d mikrotik_routeros -c lab
rusted device add core-mx -H 10.0.0.3 -d juniper_junos     -c lab

# Back up one device, or everything enabled
rusted backup run nexus1
rusted backup run --all

# Inspect results
rusted backup history nexus1
git -C "$(rusted config show | awk '/backups:/{print $2}')" log --oneline
```

## Command reference

| Command | Purpose |
|---|---|
| `rusted init` | Create the DB and backup repo |
| `rusted config init/show` | Create or display the config file |
| `rusted cred add/list/remove` | Manage login credentials |
| `rusted device add/list/remove/enable/disable` | Manage device inventory |
| `rusted driver list` | List platform drivers |
| `rusted backup run [NAME] [--all]` | Run backups |
| `rusted backup history NAME` | Show a device's backup history |
| `rusted serve` | Run the HTTP API for LibreNMS |

Global flags: `--config`, `--db`, `--backups` (each overrides the config file
and the corresponding `RUSTED_*` environment variable).

## Credential encryption

If a `secret` is configured (config file `secret`, or `RUSTED_SECRET`),
password / private-key / enable fields are encrypted with AES-256-GCM before
being written to SQLite (values are prefixed `enc:`). If it is unset, secrets
are stored in plaintext and rusted warns you. Plaintext and encrypted rows can
coexist, so you can enable encryption later — but rows written while encrypted
require the same secret to read, so **keep it stable** (the installer generates
one once and never rotates it for you).

## HTTP API / LibreNMS

The API token comes from the config file (`api_token`) or `RUSTED_API_TOKEN`:

```sh
rusted serve            # uses api_addr + api_token from config
rusted serve --addr :8080 --token "$(openssl rand -hex 32)"
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
install.sh          user/global installer
cmd/rusted/         CLI (cobra)
internal/config/    config file + env + flag resolution
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
