# Rusted LibreNMS plugin

A LibreNMS plugin that integrates [rusted](../README.md) configuration backups
into the LibreNMS web UI. It uses LibreNMS's **v2 plugin system** and talks to
the `rusted serve` HTTP API.

## What it provides

- A **Rusted Backups** menu entry.
- A single-page UI (no reloads) to **add and remove devices**.
- **Manage credentials in the web UI** — enter username/password/enable directly;
  they are sent once to rusted, encrypted at rest, and never returned to the
  browser.
- **View backup history** per device (status, timestamps, commit, detail).
- **Browse and view stored configurations**, with a **revision history** (git log)
  per device — pick any revision and read it back.
- **Compare two revisions** of a device's config with an inline unified-diff
  viewer (or view what a single revision changed over its parent).
- **Trigger a backup on demand** with inline success/failure feedback.
- A **Configuration Backups panel on every device's Overview page** showing the
  last backup, a "Back up now" button, and a one-click "View configuration" link
  (matched on hostname), plus a one-click "Add to rusted" when the device isn't
  managed yet.
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
routes/web.php                    page + AJAX receiver routes (plugin/rusted/...)
src/RustedServiceProvider.php     registers hooks, routes, views, config
src/Support/RustedClient.php      server-side HTTP client for the rusted API
src/Hooks/MenuEntry.php           menu hook
src/Hooks/Settings.php            settings hook
src/Hooks/DeviceOverview.php      per-device Overview-page panel hook
src/Controllers/RustedController.php  page + device viewer shell + JSON/AJAX receiver
resources/views/                  menu, page, device, device-overview, settings (Blade)
```

## Routes

The page is `GET plugin/rusted`. A per-device configuration viewer lives at
`GET plugin/rusted/device/{name}`. The browser's AJAX calls hit the same-origin
receiver (all `web`+`auth`+CSRF protected):

| Method & path | Relays to rusted |
|---|---|
| `GET plugin/rusted/api/devices` | `GET /api/devices` |
| `GET plugin/rusted/api/devices/{name}` | `GET /api/devices/{name}` |
| `POST plugin/rusted/api/devices` | `POST /api/devices` |
| `DELETE plugin/rusted/api/devices/{name}` | `DELETE /api/devices/{name}` |
| `POST plugin/rusted/api/devices/{name}/backup` | `POST /api/devices/{name}/backup` |
| `GET plugin/rusted/api/devices/{name}/history` | `GET /api/devices/{name}/history` |
| `GET plugin/rusted/api/devices/{name}/versions` | `GET /api/devices/{name}/versions` (revision list) |
| `GET plugin/rusted/api/devices/{name}/config?commit=<hash>` | `GET /api/devices/{name}/config` — latest, or `?commit=` for a revision (**text/plain**) |
| `GET plugin/rusted/api/devices/{name}/diff?from=<a>&to=<b>` | `GET /api/devices/{name}/diff` — unified diff (`from` alone = that revision's change; `from`+`to` = between two) (**text/plain**) |
| `GET plugin/rusted/api/credentials` | `GET /api/credentials` |
| `POST plugin/rusted/api/credentials` | `POST /api/credentials` |
| `DELETE plugin/rusted/api/credentials/{name}` | `DELETE /api/credentials/{name}` |
| `GET plugin/rusted/api/drivers` | `GET /api/drivers` |

The JSON receiver forwards rusted's JSON body and HTTP status, and turns any
transport failure into a clean `502` so the UI always gets JSON.

The `config` and `diff` receivers are relayed verbatim as **`text/plain`** (the
raw config / unified diff payload) and turn any transport failure into a clean
`502` text response, so the browser never receives JSON-wrapped text.

> Note: credentials are intentionally **not** managed from the web UI. Manage
> them with the `rusted cred` CLI and reference them by name when adding a
> device.
