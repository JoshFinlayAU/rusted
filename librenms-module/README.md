# Rusted LibreNMS plugin

A LibreNMS plugin that integrates [rusted](../README.md) configuration backups
into the LibreNMS web UI. It uses LibreNMS's **v2 plugin system** and talks to
the `rusted serve` HTTP API.

## What it provides

- A **Rusted Backups** menu entry.
- A single-page UI (no reloads) to **add and remove devices**.
- **Inline editing** of each device's **port**, **driver**, **transport**,
  **credential** and **group** directly in the table; a change is saved as soon
  as the field changes. Clearing port or group resets it to the default / no
  group.
- An **enable/disable toggle** per device. Disabling stops backups but keeps the
  device and its history.
- A **backup status** column: a green check when the last backup succeeded in
  the past 24 hours, a grey `?` when it is older, a red triangle when the last
  run failed, and a grey circle when the device was never backed up. Hover for
  the timestamp and the failure message.
- **Manage credentials in the web UI**: enter username/password/enable directly;
  they are sent once to rusted, encrypted at rest, and never returned to the
  browser.
- A **per-device page** with the stored configuration, its **revision history**
  (git log), any revision viewable on demand, a **unified diff** between two
  revisions (or what one revision changed), the **backup run history** (status,
  timestamps, bytes, commit, detail), and a download of **every revision as a
  zip**.
- **Trigger a backup on demand** with success/failure feedback.
- **Bulk device sync**: a two-pane page to add many LibreNMS devices to rusted at
  once (one driver + credential per batch) or disable them.
- A **Configuration Backups panel on every device's Overview page** showing the
  last backup with enable/disable, back up now, history and delete buttons, and a
  one-click "Add to rusted" when the device isn't managed yet (see
  [Device names](#device-names)).
- Deleting a device first **downloads its configuration history as a zip**; the
  delete proceeds only if that download succeeds (or there is nothing to
  archive and you confirm).
- Messages appear in **sticky alerts** fixed below the navbar. Success messages
  fade after 10 seconds; warnings and errors stay until closed.
- A **settings page** showing API connectivity status.

## Access control

The whole plugin is restricted to **LibreNMS administrators** (the `admin`
gate). That covers the menu entry, the pages, the device Overview panel, the
settings page and every AJAX route. It is enforced server-side twice: the routes
carry `can:admin` middleware, and the controller checks the same gate before
every action.

Read-only users get nothing, not even the device list: the plugin can change
devices and credentials, trigger backups, and serve stored configurations,
which contain secrets (password hashes, SNMP communities, keys). Changing a
device's host would also let a user send its stored credentials to a host they
control.

## Architecture — no reverse proxy needed

The browser never talks to the rusted API directly. Instead, the page makes
**same-origin AJAX calls to this plugin's own routes** under
`plugin/rusted/api/...`, served by LibreNMS itself. The plugin's controller then
relays each call to the rusted API server-side:

```
browser ──AJAX (same origin, session auth + CSRF)──▶ LibreNMS plugin route
                                                          │
                                                          ▼ (server-side)
                                            rusted API (Bearer token)
```

Because the AJAX receiver lives inside LibreNMS, you do **not** need nginx/Apache
reverse-proxy rules to expose rusted, and the rusted API only needs to be
reachable **from the LibreNMS server** (e.g. bind it to `127.0.0.1:8080`). The
bearer token stays in the LibreNMS `.env` and is never sent to the browser.

## Requirements

- LibreNMS with the v2 plugin system (`app/Plugins`, the modern plugin manager).
- A `rusted serve` instance reachable from the LibreNMS host, and its API token.
- The PHP `zip` extension, for the history zip download.

## Install

rusted's API must be running first. It can listen on loopback only, since just
the LibreNMS server needs to reach it:

```sh
rusted serve --addr 127.0.0.1:8080   # token comes from rusted's config file
```

Then, on the LibreNMS host (install directory is usually `/opt/librenms`),
install the plugin as a Composer package.

### Option A — from a published/VCS Composer package

```sh
sudo -u librenms ./lnms plugin:add athena-networks/rusted-librenms
```

### Option B — local path package (development / air-gapped)

Use this when the plugin source lives on the LibreNMS server, e.g.
`/opt/rusted-plugin/librenms-module` (the directory holding the plugin's own
`composer.json`, not its parent). The path must be absolute and readable by the
`librenms` user (e.g. `chmod o+x /opt` and `chmod -R o+rX /opt/rusted-plugin`).

1. Register a Composer path repository. Register it in Composer's **global**
   configuration, not in LibreNMS's `composer.json`; see
   [Surviving daily updates](#surviving-daily-updates) for why:

   ```sh
   sudo -u librenms php composer.phar config --global repositories.rusted \
     '{"type":"path","url":"/opt/rusted-plugin/librenms-module"}'
   ```

2. Install the package. A path repository without a VCS tag is published as a
   `dev-` version, which `@dev` matches:

   ```sh
   sudo -u librenms ./lnms plugin:add athena-networks/rusted-librenms @dev
   ```

   `./lnms plugin:add` uses LibreNMS's bundled Composer, so `composer` does not
   need to be on `PATH`.

### Configure and enable

Add the connection settings to the LibreNMS `.env`:

```ini
RUSTED_API_URL=http://127.0.0.1:8080
RUSTED_API_TOKEN=a-long-random-token
```

Enable the plugin in LibreNMS under **Settings → Plugins** (or
`./lnms plugin:enable rusted`), then clear caches:

```sh
sudo -u librenms ./lnms config:clear
sudo -u librenms php artisan view:clear
sudo -u librenms php artisan route:clear
```

Open **Rusted Backups** from the main menu (as an administrator).

### Surviving daily updates

LibreNMS updates itself daily through `daily.sh`, which first runs
`git checkout -- composer.json composer.lock`, discarding local edits, and then
reinstalls the plugins recorded in `composer.plugins.json`. A path repository
added to LibreNMS's `composer.json` is therefore wiped every night, the package
can no longer be resolved, and the plugin disappears from `vendor/`.

A repository in Composer's global config (`<COMPOSER_HOME>/config.json`,
typically `/opt/librenms/.composer/config.json`) lives outside the LibreNMS git
tree and survives. Check that it is where the nightly run will read it:

```sh
sudo -u librenms php composer.phar config --global home          # e.g. /opt/librenms/.composer
sudo -u librenms php composer.phar config --global repositories  # must list "rusted"
```

The enabled state (database) and connection settings (`.env`) survive updates
on their own.

### Uninstall

```sh
sudo -u librenms ./lnms plugin:disable rusted
sudo -u librenms ./lnms plugin:remove athena-networks/rusted-librenms
sudo -u librenms php composer.phar config --global --unset repositories.rusted   # if used
sudo -u librenms ./lnms config:clear
```

Backups are not touched: configurations and history live on the rusted server.

## Using it

### Devices page (`plugin/rusted`)

| Column | Editable |
|---|---|
| Name (links to the device page) | no |
| Host | no |
| Port | yes; empty = rusted default (22) |
| Driver | yes |
| Transport | yes; "default" = rusted's default transport |
| Credential | yes |
| Group | yes; empty = no group |
| Backup | status icon (see above) |
| Actions | enable/disable, back up now, device page, delete |

Changing a device's group moves its config file to a different directory in the
backup repository, so revisions from before the move are not listed under the
new path.

Edits only send the field that changed. The plugin reads the current device,
applies the change and posts the full record back, so fields it does not show
(such as timeouts) keep their values.

### Device page (`plugin/rusted/device/{name}`)

Shows the latest configuration with **Raw** (download what is displayed) and
**All revisions** (zip of every stored revision plus `latest.cfg` and a
`MANIFEST.txt`) buttons, then the revision list (view any revision, or diff it
against its parent), a two-revision diff, and the backup runs.

### Device names

A device added from LibreNMS is named in rusted after its **LibreNMS display
name**, so if LibreNMS shows sysName instead of hostname (the
`device_display_default` setting, or a device's own display name), so do rusted,
the backup repo and this plugin. rusted still connects to the LibreNMS
hostname/IP. The hostname is used instead when the display name isn't a valid
rusted device name (one containing `/`, for example), or when a rusted device on
another host already has it (two LibreNMS devices sharing a sysName).

An existing rusted device belongs to a LibreNMS device when, case-insensitively:

1. its name is the LibreNMS display name and its host is the LibreNMS hostname, or
2. its name is the LibreNMS hostname (devices added by IP before this), or
3. its host is the LibreNMS hostname and no other rusted device has that host
   (devices named by hand).

Existing devices are not renamed. To move one to its sysName, run
`rusted device rename OLD NEW` on the rusted server; its history follows.

### Device sync (`plugin/rusted/sync`)

Devices are matched as described in [Device names](#device-names).

- **Left:** LibreNMS devices not backed up by rusted, either absent or disabled
  in rusted.
- **Right:** enabled rusted devices that are also in LibreNMS. Rusted devices
  that are not in LibreNMS are listed greyed and cannot be selected.

**Add to rusted** applies one driver and one credential to the selected devices
(re-enabling disabled ones). **Disable in rusted** stops their backups without
deleting anything. Devices that fail are highlighted red and the errors are
listed. Each pane has a regex filter.

## Layout

```
composer.json                     package manifest (registers the provider)
config/config.php                 api_url / api_token / timeout (from .env)
routes/web.php                    pages + AJAX receiver routes (plugin/rusted/...)
src/RustedServiceProvider.php     registers hooks, routes, views, config
src/Support/RustedClient.php      server-side HTTP client for the rusted API
src/Hooks/MenuEntry.php           menu hook
src/Hooks/Settings.php            settings hook
src/Hooks/DeviceOverview.php      per-device Overview-page panel hook
src/Controllers/RustedController.php  page shells + AJAX receiver
resources/views/                  menu, page, device, sync, device-overview, settings
resources/views/partials/         sticky-alerts (shared alert container)
```

## Routes

Pages: `GET plugin/rusted`, `GET plugin/rusted/device/{name}` and
`GET plugin/rusted/sync`. The browser's AJAX calls hit the same-origin receiver.
Every route is `web` + `auth` + `can:admin`, and state-changing requests are
CSRF-protected.

| Method & path | Relays to rusted |
|---|---|
| `GET plugin/rusted/api/devices` | `GET /api/devices` |
| `GET plugin/rusted/api/devices/{name}` | `GET /api/devices/{name}` |
| `POST plugin/rusted/api/devices` | `POST /api/devices` |
| `PUT plugin/rusted/api/devices/{name}` | `GET /api/devices/{name}`, then `POST /api/devices` with only the sent fields changed |
| `POST plugin/rusted/api/devices/{name}/enabled` | same read-modify-write, changing only `enabled` |
| `DELETE plugin/rusted/api/devices/{name}` | `DELETE /api/devices/{name}` |
| `POST plugin/rusted/api/devices/{name}/backup` | `POST /api/devices/{name}/backup` |
| `GET plugin/rusted/api/devices/{name}/history` | `GET /api/devices/{name}/history` |
| `GET plugin/rusted/api/devices/{name}/history/zip` | builds a zip from `/versions` and `/config?commit=` |
| `GET plugin/rusted/api/devices/{name}/versions` | `GET /api/devices/{name}/versions` |
| `GET plugin/rusted/api/devices/{name}/config?commit=` | `GET /api/devices/{name}/config` (text/plain) |
| `GET plugin/rusted/api/devices/{name}/diff?from=&to=` | `GET /api/devices/{name}/diff` (text/plain) |
| `GET plugin/rusted/api/drivers` | `GET /api/drivers` |
| `GET plugin/rusted/api/transports` | `GET /api/transports` (an empty list if rusted lacks it) |
| `GET plugin/rusted/api/credentials` | `GET /api/credentials` |
| `POST plugin/rusted/api/credentials` | `POST /api/credentials` |
| `DELETE plugin/rusted/api/credentials/{name}` | `DELETE /api/credentials/{name}` |
| `GET plugin/rusted/api/sync/state` | joins LibreNMS devices with `GET /api/devices` |
| `POST plugin/rusted/api/sync/add` | read-modify-write `POST /api/devices` per device |
| `POST plugin/rusted/api/sync/disable` | read-modify-write `POST /api/devices` per device |

The JSON receiver forwards rusted's body and HTTP status, and turns any
connection failure into a clean `502` so the UI always gets JSON. `config` and
`diff` are relayed as raw `text/plain`.
