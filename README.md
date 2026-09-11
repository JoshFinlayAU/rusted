# rusted

A network device configuration backup tool - a modern, single-binary
replacement for **RANCID** and **Oxidized**.

rusted connects to your network devices over SSH, captures their running
configuration using per-platform drivers, masks volatile data so backups are
diff-stable, and versions every change in a git repository. Credentials and
device inventory live in a local SQLite database, and an HTTP API drives the
bundled **LibreNMS** integration.

## Features

- **SSH and telnet transports**, with a documented, pluggable transport interface
  (NETCONF/REST/serial can be added without touching the core) — see
  [docs/transport-modules.md](docs/transport-modules.md).
- **Cisco enable-mode support**: when a credential has an `enable` password
  configured, rusted sends `enable` and responds to the `Password:` prompt
  automatically (for `cisco_ios`, `cisco_asa`, etc.).
- **Per-device timeout tuning**: slow or high-latency devices can override the
  command timeout (`--cmd-timeout`) and idle timeout (`--idle-timeout`) so slow
  output is not truncated prematurely.
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

Also bundled: `cisco_ios` (with enable-mode escalation), `cisco_asa`, `arista_eos`,
`fortinet`, `openwrt`, `vyos`, `generic`. Run `rusted driver list` to see them all.

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

# Cisco IOS over telnet with longer timeouts for a slow device
rusted device add ios-1  -H 10.0.0.4 -d cisco_ios -c lab \
  --transport telnet -P 23 --cmd-timeout 120 --idle-timeout 2000

# Update an existing device (change transport, driver, timeouts, etc.)
rusted device update ios-1 --transport telnet --cmd-timeout 120

# Rename a device, keeping its history. This moves the config file in the
# backup repo (git mv) and reuses the same DB row, so old backups stay
# viewable/diffable and 'backup history' survives under the new name — use it
# instead of add-new + remove-old, which would drop the history.
rusted device rename old-name new-name

# Back up one device, or everything enabled
rusted backup run nexus1
rusted backup run --all

# Inspect results
rusted backup history nexus1
git -C "$(rusted config show | awk '/backups:/{print $2}')" log --oneline

# Delete a device: its config file is removed from the backup repository too.
# Add --purge-history to also rewrite the repository so no trace of the
# device's configurations remains (irreversible; requires git-filter-repo).
rusted device remove ios-1 --purge-history
```

## Command reference

| Command | Purpose |
|---|---|
| `rusted init` | Create the DB and backup repo |
| `rusted config init/show` | Create or display the config file |
| `rusted cred add/list/remove` | Manage login credentials |
| `rusted device add/list/remove/rename/update/enable/disable` | Manage device inventory |
| `rusted driver list` | List platform drivers |
| `rusted backup run [NAME] [--all]` | Run backups |
| `rusted backup run NAME --raw` | Run a backup saving the config verbatim (volatile fields included) |
| `rusted backup run -v` | Run backups with verbose stderr progress |
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
| `GET /api/devices` | List devices (includes `last_backup` timestamp + `last_status` of the most recent run) |
| `POST /api/devices` | Add or re-register a device (JSON; upsert on name) |
| `GET /api/devices/{name}` | Device detail |
| `DELETE /api/devices/{name}` | Remove a device, its run history **and its config file from the backup repository** (a removal commit is recorded). Add `?purge=true` to also erase every stored version from the repository's git history — irreversible, needs `git-filter-repo`, rewrites all commit hashes (recorded run hashes are remapped automatically); answers 501 if the tool is missing |
| `GET /api/devices/{name}/history` | Backup history (50 most recent runs) |
| `GET /api/devices/{name}/versions` | Git revisions of the device's config, newest first: `commit`, `date`, `captured_at` (RFC3339), `author`, `subject` |
| `GET /api/devices/{name}/config` | Latest stored config (text) |
| `POST /api/devices/{name}/backup` | Trigger a backup now |
| `GET /api/credentials` | List credentials (no secrets returned) |
| `POST /api/credentials` | Add a credential (name, username, password, enable) |
| `DELETE /api/credentials/{name}` | Remove a credential |
| `GET /api/drivers` | List platform drivers |
| `GET /api/transports` | List registered transports (e.g. `ssh`, `telnet`, `ssh-exec`) |

The device `GET /api/devices` and `GET /api/devices/{name}` endpoints return
`last_backup` (RFC 3339 timestamp of the most recent backup run, or `null` if none)
and `last_status` (`"success"`, `"unchanged"`, `"failed"`, or `""` if no runs exist).

The credential `GET` only reports whether a password/key/enable is set — it never
returns secret material.

The LibreNMS plugin that consumes this API lives in
[`librenms-module/`](librenms-module/README.md).

## How change detection works

For each backup, rusted runs the driver's config commands, applies the driver's
line `Strip` rules, masks dynamic substrings (`internal/normalize`), then writes
the result to `backups/<group>/<device>.cfg`. It commits **only if the file
content actually changed** — so timestamps, uptimes, and "last changed" banners
never create noise in your git history. A run is recorded as `success`
(committed), `unchanged` (no diff), or `failed`.

Some platforms also rewrite parts of the configuration on every save even when
nothing really changed. Drivers declare those fields, and they are **ignored by
default**: the `fortinet` driver, for example, drops FortiOS's rotating
`#conf_file_ver=` header and re-encrypted secrets such as `set password ENC …`
(each save produces fresh ciphertext that cannot be decrypted back, so keeping
it would only create spurious commits). Encrypted PEM blocks are skipped too —
FortiGate re-encrypts stored private keys with a fresh salt/IV on every save,
so `set private-key "-----BEGIN ENCRYPTED PRIVATE KEY-----…` changes wholesale
between identical dumps. Plain certificates and public keys are deterministic,
so they stay in the backup.

To capture a configuration **verbatim**, ignored fields included, pass `--raw`.
It skips both the per-driver ignore rules and inline timestamp masking, storing
exactly what the device emitted:

```sh
rusted backup run fgt-cluster --raw   # complete capture, volatile fields included
```

### Backup error reporting

When a backup fails, rusted prints the failure reason to **stderr** (even without
`--verbose`) and exits non-zero so schedulers/sensors catch it:

```
FAIL core-sw1: connect: dial tcp 10.0.0.1:23: connection refused
FAIL edge-fw: init command "enable": Password: prompt but no enable password
FAIL ios-1: config command "show running-config": command "show running-config" timed out after 60s
```

**Timeouts** are detected in two ways:

- **Hard timeout** — the command runs longer than `--cmd-timeout` (default 60 s).
- **Idle timeout** — the device stops sending for `--idle-timeout` (default 5 s)
  *without* returning to a prompt, indicating the command did not complete
  (e.g. a large `show running-config` with natural pauses between bursts).

Both produce `command %q timed out after %s`, so you see a clear timeout
instead of the misleading "captured empty configuration". To fix, raise the
per-device `--cmd-timeout` (for slow devices) or `--idle-timeout` (for devices
that pause mid-output):

```sh
rusted device update slow-sw --cmd-timeout 180 --idle-timeout 10000
```

**Pagination** (`--More--`) is handled automatically: when a device emits a
pager prompt, the transport sends a space to advance past it without treating
it as a timeout. This works for both SSH and telnet.

**Truncated dumps fail loudly.** If the device (or the network) drops the
session mid-dump — FortiGate HA clusters do this occasionally under load — the
run is recorded as `failed` and **nothing is committed**, instead of silently
saving a partial capture as a valid backup:

```
FAIL fgt-cluster: config command "show full-configuration": command "show full-configuration" did not complete: device closed the session mid-output (captured 262144 bytes)
```

Large configurations can also exceed the 60 s hard timeout before finishing;
raise the per-device `--cmd-timeout` for them (see above).

For real-time progress on a long run, use `--verbose` (`-v`):

```
rusted backup run core-sw1 -v
```

```
=== core-sw1 [10.0.0.1:cisco_ios] ===
  using transport "ssh", driver "cisco_ios"
  connected, running 3 init command(s)
  init commands ok, collecting config (1 command(s))
  captured 4240 bytes, saving to git...
```

For full diagnostic output including raw device I/O (every command sent and
every byte received), use `--debug`:

```
rusted backup run core-sw1 --debug
```

```
=== core-sw1 [10.0.0.1:cisco_ios] ===
  using transport "ssh", driver "cisco_ios"
  connected, running 3 init command(s)
>>> enable
<<< Password:
>>> [enable password]
<<< core-sw1#
>>> terminal length 0
<<< core-sw1#
>>> terminal width 0
<<< core-sw1#
>>> show running-config
<<< Building configuration...
<<< version 12.2
<<< ...
<<< 4240 bytes captured
  captured 4240 bytes, saving to git...
```

Lines prefixed `>>>` are commands sent to the device; `<<<` are responses
received. Secrets (enable passwords) are masked as `[enable password]`.

Use `rusted backup history NAME` to see the full recorded message for any past
run (including failures).

## Scheduled backups

`rusted backup run --all` backs up every enabled device, then exits — ideal for
a scheduler. It exits non-zero if any device failed, so cron/systemd surface
failures. Always pass `--config` (a scheduler's environment is minimal) and an
absolute path to the binary.

### Option A — cron

Edit the crontab of the user that owns the data directory (`crontab -e`, or
`sudo crontab -e` for a global install owned by root):

```cron
# Back up all devices every day at 02:00, one run at a time, with a log.
0 2 * * * /usr/bin/flock -n /tmp/rusted-backup.lock \
  /usr/local/bin/rusted --config /etc/rusted/config.toml backup run --all \
  >> /var/log/rusted-backup.log 2>&1
```

- `flock -n` prevents a slow run from overlapping the next one.
- Add `MAILTO=you@example.com` at the top of the crontab to be emailed on
  failure (cron mails any output; the non-zero exit also flags it).
- For a **user install**, use your paths instead, e.g.
  `~/.local/bin/rusted --config ~/.config/rusted/config.toml backup run --all`.

### Option B — systemd timer

More robust than cron (logging via journald, no overlap, easy status). Create
two unit files:

`/etc/systemd/system/rusted-backup.service`:

```ini
[Unit]
Description=rusted — back up all network device configs
After=network-online.target
Wants=network-online.target

[Service]
Type=oneshot
ExecStart=/usr/local/bin/rusted --config /etc/rusted/config.toml backup run --all
# User=rusted        # if you run rusted under a dedicated account
```

`/etc/systemd/system/rusted-backup.timer`:

```ini
[Unit]
Description=Run rusted backups on a schedule

[Timer]
OnCalendar=*-*-* 02:00:00
Persistent=true          # catch up if the machine was off at 02:00
RandomizedDelaySec=300   # optional: jitter to avoid hammering devices at once

[Install]
WantedBy=timers.target
```

Then enable it:

```sh
sudo systemctl daemon-reload
sudo systemctl enable --now rusted-backup.timer
systemctl list-timers rusted-backup.timer   # confirm next run
journalctl -u rusted-backup.service         # view backup output
```

Run it once by hand to verify: `sudo systemctl start rusted-backup.service`.

> Tip: this is independent of `rusted serve`. You can run the API service
> (`--service` in the installer) *and* a backup timer side by side.

## Per-device timeouts

Network devices vary widely in how fast they emit output. rusted exposes two
per-device timeout overrides that override the engine-wide defaults (60s command
timeout, 5s idle window):

| Flag | Stored as | Default | When to raise it |
|---|---|---|---|
| `--cmd-timeout SECONDS` | `cmd_timeout` | 60 | Large configs on slow CPUs that take >60s to dump |
| `--idle-timeout MILLISECONDS` | `idle_timeout` | 5000 | Devices that pause >5s between output bursts, causing premature truncation |

Example:

```sh
rusted device add legacy-sw --host 10.0.0.5 -d cisco_ios -c lab --transport ssh \
  --cmd-timeout 180 --idle-timeout 10000
```

These are also available on `rusted device update`. Use `rusted device list` to
inspect configured timeouts per device.

## Project layout

```
install.sh          user/global installer
cmd/rusted/         CLI (cobra)
internal/config/    config file + env + flag resolution
internal/store/     SQLite: credentials, devices, run history
internal/secret/    AES-GCM encryption-at-rest
internal/transport/ transport interface + SSH + telnet implementations
internal/driver/    per-platform drivers
internal/normalize/ dynamic-string (timestamp/date/uptime) masking
internal/gitstore/  git-backed backup storage
internal/backup/    backup engine
internal/api/       HTTP API for LibreNMS
librenms-module/    LibreNMS plugin
docs/               transport & driver authoring guides
```

## Roadmap

- `known_hosts` host-key pinning (SSH currently accepts any host key).
- Concurrent `--all` backups with a worker pool.
- Webhook/Slack notifications on backup failure.
- NETCONF transport.
