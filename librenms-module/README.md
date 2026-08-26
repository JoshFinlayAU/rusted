# Rusted LibreNMS plugin

A LibreNMS plugin that integrates [rusted](../README.md) configuration backups
into the LibreNMS web UI. It uses LibreNMS's **v2 plugin system** and talks to
the `rusted serve` HTTP API.

## What it provides

- A **Rusted Backups** menu entry.
- A single-page UI (no reloads) to **add and remove devices**.
- **Inline editing** of device **driver**, **transport**, and **port** directly
  in the table — changes are saved immediately with no separate edit form.
- A **backup status** column on the Devices page showing at a glance whether each
  device's config was backed up within the last 24 hours (green), is stale
  (yellow), failed (red), or has never been backed up (grey). Hover for exact
  timestamps and messages.
- **Manage credentials in the web UI** — enter username/password/enable directly;
  they are sent once to rusted, encrypted at rest, and never returned to the
  browser.
- **View backup history** per device (status, timestamps, commit, detail).
- **Browse and view stored configurations**, with a **revision history** (git log)
  per device — pick any revision and read it back, or **download every revision**
  of a device as a zip.
- **Compare two revisions** of a device's config with an inline unified-diff
  viewer (or view what a single revision changed over its parent).
- **Bulk device sync** — a two-pane view (LibreNMS-only ↔ managed by rusted) to
  add many devices to rusted in one operation (a single driver + credential per
  batch, per family), or **disable** them. Orphaned rusted devices (not in
  LibreNMS) are shown greyed and locked. Disabling a device stops backups but
  **preserves its configuration history** (it is never deleted from rusted), and
  the device reappears on the left so it can be re-enabled; devices that fail to
  add/disable stay put and are highlighted red.
- **Trigger a backup on demand** with inline success/failure feedback.
- A **Configuration Backups panel on every device's Overview page** showing the
  last backup and exactly four icon buttons (hover for the label): an
  **Enabled toggle** (enable/disable — soft, history preserved), **Back up now**
  (runs a backup, surfaces any error), **Show history** (opens the configuration
  viewer — revisions + diff), and **Remove** (downloads every configuration
  revision as a zip, *then* deletes the device and purges its files from the
  backup repository). It also offers a one-click
  "Add to rusted" when the device isn't managed yet.
- A **settings page** showing API connectivity status.

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

## Install

`rusted`'s API must be running first. It can listen on loopback only, since just
the LibreNMS server needs to reach it:

```sh
rusted serve --addr 127.0.0.1:8080   # token comes from rusted's config file
```

Then, on the LibreNMS host (install dir is usually `/opt/librenms`), install the
plugin. Two options are available.

### Option A — from a published/VCS Composer package

```sh
./lnms plugin:add athena-networks/rusted-librenms
```

### Option B — local path package (development / air-gapped)

This is the recommended approach when the plugin source lives on disk on the
LibreNMS server (e.g. `/opt/rusted-plugin/librenms-module`, the directory that
contains the plugin's own `composer.json`).

1. **Point a Composer path repository at the plugin directory.** Add a
   `repositories` entry to the LibreNMS `composer.json`. The path must be
   **absolute** and readable by the `librenms` user:

   ```sh
   jq '.repositories = [{"type":"path","path":"/opt/rusted-plugin/librenms-module"}]' \
       composer.json > composer.json.tmp && mv composer.json.tmp composer.json
   ```

   Or, by hand:

   ```json
   "repositories": [
       { "type": "path", "path": "/opt/rusted-plugin/librenms-module/" }
   ]
   ```

   > **Schema note:** the directory must end at the folder that holds the
   > plugin's `composer.json` (i.e. the `librenms-module` folder, not its
   > parent).
   >
   > **Warning:** an entry written directly into `composer.json` does **not**
   > survive LibreNMS's nightly self-update — see
   > [Making the install persistent](#making-the-install-persistent-across-daily-updates)
   > below to register the repository where updates cannot touch it.

2. **Install the package.** Always pass a version constraint; a path repository
   without a VCS tag is published as `dev-main`/`dev-master`, which `@dev`
   matches:

   ```sh
   sudo -u librenms ./lnms plugin:add athena-networks/rusted-librenms @dev
   ```

   `./lnms plugin:add` invokes LibreNMS's bundled Composer wrapper internally, so
   the `composer` binary does **not** need to be on `PATH`.

3. **Enable the plugin.** The plugin registers itself under the name `rusted`
   (see `RustedServiceProvider::PLUGIN`), so:

   ```sh
   sudo -u librenms ./lnms plugin:enable rusted
   ```

   (If the package was just installed via `plugin:add`, it is already registered
   in `composer.plugins.json` and may print "Plugin already enabled" — that is
   fine.)

4. **Clear caches** so the new routes, views and config take effect:

   ```sh
   sudo -u librenms ./lnms config:clear
   sudo -u librenms php artisan view:clear
   sudo -u librenms php artisan route:clear
   ```

Add the connection settings to the LibreNMS `.env`:

```ini
RUSTED_API_URL=http://127.0.0.1:8080
RUSTED_API_TOKEN=a-long-random-token
```

Open **Rusted Backups** from the main menu.

#### Making the install persistent across daily updates

LibreNMS auto-updates itself once a day through `daily.sh` (run by cron). Before
pulling new code it restores Composer's files from git, silently discarding every
local modification (`daily.sh`):

```bash
# Restore composer files if user installed plugins
git checkout --quiet -- composer.json composer.lock
```

Afterwards it re-installs the packages recorded in the untracked, update-safe
`composer.plugins.json` and runs `composer install`. That mechanism keeps plugins
installed across updates — but package resolution happens *after* the reset above.
A path repository added by hand to `composer.json` (step 1 of the basic procedure)
is therefore wiped every night: the package can no longer be resolved, and the
next morning both the repository entry and the plugin are gone from `vendor/`.

To make the installation persistent, register the path repository in Composer's
**global** configuration instead — `<COMPOSER_HOME>/config.json`, typically
`/opt/librenms/.composer/config.json` — which lives outside the LibreNMS git tree
and survives every reset. Adjust the URL to the real path on your server (same
rules as before: absolute, readable by `librenms`, ending at the folder that holds
the plugin's own `composer.json`, not its parent):

```sh
sudo -u librenms php composer.phar config --global repositories.rusted \
  '{"type":"path","url":"/opt/rusted-plugin/librenms-module"}'
```

Then (re)install the package once exactly as in steps 2–4 above. From then on the
nightly flow resolves the package through the global repository and reinstalls
the plugin automatically — nothing else needs redoing: the enabled state lives in
the database and the connection settings (`RUSTED_API_URL` / `RUSTED_API_TOKEN`)
live in `.env`; both survive updates untouched.

Verify that the repository was written where the nightly cron will read it:

```sh
sudo -u librenms php composer.phar config --global home         # e.g. /opt/librenms/.composer
sudo -u librenms php composer.phar config --global repositories # must show "rusted"
```

To confirm end-to-end persistence, watch the first daily run after installing:
`grep 'Updating Composer packages' logs/daily.log` must end in `OK`, and
`vendor/athena-networks/rusted-librenms` must still exist the following morning.

#### Other local plugins and multiple repositories

The global configuration holds any number of named repositories — each local
plugin gets its own unique name under `repositories.<name>`, so several plugins
coexist without interfering:

```sh
sudo -u librenms php composer.phar config --global repositories.rusted \
  '{"type":"path","url":"/opt/rusted-plugin/librenms-module"}'
sudo -u librenms php composer.phar config --global repositories.myother \
  '{"type":"path","url":"/opt/other-plugin/librenms-module"}'
```

- Re-running a command with an existing name replaces **only** that entry; other
  plugins' repositories are preserved.
- List what is registered: `php composer.phar config --global repositories`.
- Packages published on Packagist need no repository entry at all — global
  entries are only needed for local-path or private VCS packages.
- Composer builds that enforce the stricter schema requiring `$url` instead of
  `$path` behave identically here (see [Troubleshooting](#troubleshooting)).

#### Uninstalling

Remove in this order, as the `librenms` user from the LibreNMS directory:

```sh
cd /opt/librenms

# 1. deactivate the plugin (database/UI)
sudo -u librenms ./lnms plugin:disable rusted

# 2. uninstall the package (cleans composer.plugins.json AND composer.json)
sudo -u librenms ./lnms plugin:remove athena-networks/rusted-librenms

# 3. drop the global path repository (if registered)
sudo -u librenms php composer.phar config --global --unset repositories.rusted

# 4. clear caches
sudo -u librenms ./lnms config:clear
sudo -u librenms php artisan view:clear
sudo -u librenms php artisan route:clear
```

Notes:

- If step 2 prints *"athena-networks/rusted-librenms is not installed"* (possible
  when the package had already been wiped by a broken nightly run), the error is
  harmless: `composer.plugins.json` is cleaned regardless — proceed with step 3.
- If the repository was instead added directly to `composer.json` (basic
  procedure above), remove that entry by hand or restore the file with
  `git checkout -- composer.json` — the next nightly update would do it anyway.
- Optionally also remove `RUSTED_API_URL` / `RUSTED_API_TOKEN` from `.env` and
  delete the plugin source directory itself.
- Backups are **not** touched: configurations and history live on the `rusted`
  server, not in LibreNMS — reinstalling later simply reconnects to them.

### The Devices page

The main page lists every device rusted knows about. Each row in the table is
**directly editable** — click into any **Driver**, **Transport**, or **Port** cell
to change it; the change is saved immediately (debounced 500 ms for text inputs).
A color-coded **Backup** column shows the last backup's status at a glance:

| Icon | Meaning |
|---|---|
| Green check ✔ **UP TO DATE** | last run was **success** or **unchanged**, within the last 24 hours |
| Grey question mark ? | last run was **success** or **unchanged**, but more than 24 hours ago (stale) |
| Red triangle ⚠ **failed** | last backup run failed (tooltip shows the failure message) |
| Grey circle ○ **never** | no backups have ever been recorded |

Hover any icon for the exact timestamp and message. The **Enabled** state of each
device is shown only via the toggle button in the **Actions** column (see below):

| Column | What it shows | Editable? |
|---|---|---|
| Name | device name | no |
| Host | hostname/IP | no |
| Port | management port | **yes** (inline number input) |
| Driver | platform driver | **yes** (inline dropdown) |
| Transport | ssh / telnet / ssh-exec / etc. | **yes** (inline dropdown) |
| Auth | credential name | **yes** (inline dropdown) |
| Group | backup sub-directory | no |
| Backup | last run status + timestamp | no (tooltip details) |
| Actions | enable/disable, backup, history, delete | — |

Every row ends in the **four icon buttons**:

| Button | Does |
|---|---|
| Enabled toggle (🔌) | enable ↔ disable (soft — never deletes; keeps history) |
| Back up now (🔽) | triggers an immediate backup; surfaces any error |
| Show history (📜) | opens that device's configuration viewer (revisions + config + diff) |
| Delete (🗑) | opens a dialog: the **full configuration history is downloaded as a zip first**, then the device is deleted from rusted and its config file removed from the backup repository; an optional **"Irreversible purge"** checkbox also erases every stored version from the git history (needs `git-filter-repo` on the rusted host). If the download fails, nothing is deleted |

Use the **Device sync** button (top right) to bulk-add or bulk-disable LibreNMS
devices, and **Export all** to download every device's full history in one zip.

### Verifying the install

There is no `lnms plugin:list` command. To confirm the plugin is registered and
enabled, query the database directly:

```sh
sudo -u librenms php lnms db:query \
  "SELECT plugin_name, plugin_active, version FROM plugins WHERE plugin_name LIKE 'rusted%';"
```

`plugin_active` should be `1`. If the provider discovery step ran, the
`RustedServiceProvider` will also appear in `./lnms package:discover` output.

### Troubleshooting

#### `The property url is required` / `type does not have a value in the enumeration ["path"]`

Some Composer builds ship a stricter JSON schema for path repositories that
expects the property **`$url`** instead of `$path`. If `composer validate` or
`lnms plugin:add` fails with:

```
repositories[0].url : The property url is required
repositories[0].type : Does not have a value in the enumeration ["path"]
```

swap the property name from `path` to `url`:

```sh
jq '.repositories = [{"type":"path","url":"/opt/rusted-plugin/librenms-module"}]' \
    composer.json > composer.json.tmp && mv composer.json.tmp composer.json
```

i.e. the entry becomes `{"type": "path", "url": "/opt/rusted-plugin/librenms-module"}`.
Both forms behave identically for path repositories (Composer symlinks the
directory); only the property name differs by schema version.

#### `composer: command not found`

Use `./lnms plugin:add ...` (above) instead of invoking `composer` directly —
`lnms` always uses LibreNMS's bundled `composer.phar` via
`scripts/composer_wrapper.php`, so no global Composer install is required.

#### Permission denied on the plugin path

The `librenms` system user must be able to read (and stat) every directory in
the path to the plugin, e.g. `chmod o+x /opt` and `chmod -R o+rX
/opt/rusted-plugin`.

## Layout

```
composer.json                     package manifest (registers the provider)
config/config.php                 api_url / api_token / timeout (from .env)
routes/web.php                    page + device viewer + bulk-sync routes (plugin/rusted/...)
src/RustedServiceProvider.php     registers hooks, routes, views, config
src/Support/RustedClient.php      server-side HTTP client for the rusted API
src/Hooks/MenuEntry.php           menu hook
src/Hooks/Settings.php            settings hook
src/Hooks/DeviceOverview.php      per-device Overview-page panel hook (View configuration button)
src/Controllers/RustedController.php  page + device viewer + bulk-sync shells + JSON/AJAX/text receiver
resources/views/                  menu, page, device, sync, device-overview, settings (Blade)
```

## Routes

The page is `GET plugin/rusted`. A per-device configuration viewer lives at
`GET plugin/rusted/device/{name}`, and the bulk device-sync page at
`GET plugin/rusted/sync`. The browser's AJAX calls hit the same-origin
receiver (all `web`+`auth`+CSRF protected):

| Method & path | Relays to rusted |
|---|---|
| `GET plugin/rusted/api/devices` | `GET /api/devices` (includes `last_backup` and `last_status` per device) |
| `GET plugin/rusted/api/devices/{name}` | `GET /api/devices/{name}` |
| `POST plugin/rusted/api/devices` | `POST /api/devices` (create) |
| `PUT plugin/rusted/api/devices/{name}` | partial update (GET current, merge, POST upsert) — used for inline Driver/Transport/Port edits |
| `DELETE plugin/rusted/api/devices/{name}` | `DELETE /api/devices/{name}` — add `?purge=true` to irreversibly erase the device's configs from the backup repository history too (needs `git-filter-repo`; 501 if unavailable) |
| `POST plugin/rusted/api/devices/{name}/backup` | `POST /api/devices/{name}/backup` |
| `GET plugin/rusted/api/devices/{name}/history` | `GET /api/devices/{name}/history` |
| `GET plugin/rusted/api/devices/{name}/history/zip` | `GET` device's version history built into a zip (every revision + latest, plus a `MANIFEST.txt` with the git metadata — commit, capture timestamp, author, subject — of each version) |
| `GET plugin/rusted/api/export/zip` | bulk export: **every** device's full history in a single zip (`rusted-configs-<timestamp>.zip`), one folder per device with its own `MANIFEST.txt`, plus a top-level index |
| `POST plugin/rusted/api/devices/{name}/enabled` | flips the device's `enabled` flag via upsert (preserves name/driver/transport/credential/group; history kept) |
| `GET plugin/rusted/api/devices/{name}/versions` | `GET /api/devices/{name}/versions` (revision list) |
| `GET plugin/rusted/api/devices/{name}/config?commit=<hash>` | `GET /api/devices/{name}/config` — latest, or `?commit=` for a revision (**text/plain**) |
| `GET plugin/rusted/api/devices/{name}/diff?from=<a>&to=<b>` | `GET /api/devices/{name}/diff` — unified diff (`from` alone = that revision's change; `from`+`to` = between two) (**text/plain**) |
| `GET plugin/rusted/api/drivers` | `GET /api/drivers` (populates the Driver dropdown) |
| `GET plugin/rusted/api/transports` | `GET /api/transports` (populates the Transport dropdown) |
| `GET plugin/rusted/api/credentials` | `GET /api/credentials` |
| `POST plugin/rusted/api/credentials` | `POST /api/credentials` |
| `DELETE plugin/rusted/api/credentials/{name}` | `DELETE /api/credentials/{name}` |
| `GET plugin/rusted/api/sync/state` | joins LibreNMS devices with rusted → `{left, right, grey}` |
| `POST plugin/rusted/api/sync/add` | batches `POST /api/devices` (single driver + credential, per family) |
| `POST plugin/rusted/api/sync/disable` | sets `enabled=false` per device (history preserved) |

The JSON receiver forwards rusted's JSON body and HTTP status, and turns any
transport failure into a clean `502` so the UI always gets JSON.

The `config` and `diff` receivers are relayed verbatim as **`text/plain`** (the
raw config / unified diff payload) and turn any transport failure into a clean
`502` text response, so the browser never receives JSON-wrapped text.

> Note: credentials are intentionally **not** managed from the web UI. Manage
> them with the `rusted cred` CLI and reference them by name when adding a
> device.

## Bulk device sync

A two-pane view at **Rusted Backups → Device sync** moves LibreNMS devices
into and out of rusted in batches. Devices are matched by **LibreNMS hostname
== rusted device name** (case-insensitive).

- **Left pane** — LibreNMS devices that are *not* actively backed up by rusted:
  either absent from rusted, or present but **disabled**. A `disabled` badge
  marks devices already in rusted (re-adding them re-enables them).
- **Right pane** — rusted devices that are **enabled** and also in LibreNMS
  (selectable to disable). Each row has a **View configuration** link.
- **Greyed (locked)** rows — rusted devices (any state) that are **not** in
  LibreNMS; shown for awareness but not selectable.

Each pane has a **regex filter** (hostname / os / driver) above it. The → and
← buttons move the selected rows between the panes:

- **Add to rusted (→):** pick one **driver** and one **credential** (applied to
  the whole batch, so you add one family of devices at a time). For devices
  already in rusted (disabled), their group and transport are preserved while
  re-enabling them. Devices that fail to add **stay on the left and are
  highlighted red**, with the rusted error message.
- **Disable in rusted (←):** confirms the consequence, then sets
  `enabled=false` per device. **Disabling never deletes a device or its
  history** — backups simply stop, the device is hidden from the right pane, and
  it reappears on the left (re-enableable via Add). Devices that fail to
  disable remain managed and are highlighted red.

## Downloading a device's full history

On any device's configuration viewer page
(`plugin/rusted/device/{name}`), the **All revisions** button downloads a zip
of that device's **every git revision** (`name/<commit>.cfg`) plus
`name/latest.cfg`. This is the recovery path for an accidental disable or
re-install — the contents can be re-imported into the rusted backup repo.

> True in-place restore (re-importing history into rusted after a device removal)
> is not supported by the `rusted` API today. A small rusted backend feature —
> a soft-delete/archive or a "restore config at commit" endpoint — would enable
> it; the zip above is a recoverable raw-config fallback.

## Per-device panel controls

Every device's LibreNMS **Overview** page shows a *Configuration Backups* panel
(matched on the LibreNMS hostname == the rusted device name). When the device is
managed by rusted, its three status/action buttons are **icon-only** (hover to
read the label), plus an enable/disable toggle and a delete:

| Button | Does | Preserves history? |
|---|---|---|
| Enabled toggle (🔌) | enable ↔ disable (soft) | **yes** — just stops/resumes backups |
| Back up now (🔽) | triggers a backup immediately; shows an error if it fails | — |
| Show history (📜) | opens the viewer with revisions + config/diff | — |
| Remove (🗑) | opens a dialog with an optional **Irreversible purge** checkbox; downloads a zip of every revision first, then deletes the device from rusted and removes its config file from the backup repository (purge also erases the git history) | archive saved first; device then gone (also from `./backups`, or entirely with purge) |

The **Remove** button is the only destructive action here, and it only proceeds
once the configuration archive has been handed to the browser as a download. If
that download fails, the device is **not** deleted, so nothing is ever lost by
accident. The bulk-sync page (**Device sync**), by contrast, only ever
**disables** (never deletes) — it lives in the same history-preserving model.
