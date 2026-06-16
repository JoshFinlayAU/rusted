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
- **Trigger a backup on demand** with inline success/failure feedback.
- A **Configuration Backups panel on every device's Overview page** showing the
  last backup, a "Back up now" button, and a one-click "Add to rusted" when the
  device isn't managed yet (matched on hostname).
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

rusted's API must be running first. It can listen on loopback only, since just
the LibreNMS server needs to reach it:

```sh
rusted serve --addr 127.0.0.1:8080   # token comes from rusted's config file
```

Then, on the LibreNMS host, install the plugin as a Composer package. From the
LibreNMS install directory (usually `/opt/librenms`):

```sh
# Option A — from a published/VCS Composer package
./lnms plugin:add athena-networks/rusted-librenms

# Option B — local path package (development / air-gapped)
# Add a path repository pointing at this directory to LibreNMS composer.json,
# then: composer require athena-networks/rusted-librenms:@dev
```

Add the connection settings to the LibreNMS `.env`:

```ini
RUSTED_API_URL=http://127.0.0.1:8080
RUSTED_API_TOKEN=a-long-random-token
```

Enable the plugin in LibreNMS under **Settings → Plugins** (or
`./lnms plugin:enable rusted`), then clear caches:

```sh
./lnms config:clear
php artisan view:clear
```

Open **Rusted Backups** from the main menu.

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
src/Controllers/RustedController.php  page shell + JSON AJAX receiver
resources/views/                  menu, page, device-overview, settings (Blade)
```

## Routes

The page is `GET plugin/rusted`. The browser's AJAX calls hit the same-origin
receiver (all `web`+`auth`+CSRF protected):

| Method & path | Relays to rusted |
|---|---|
| `GET plugin/rusted/api/devices` | `GET /api/devices` |
| `GET plugin/rusted/api/devices/{name}` | `GET /api/devices/{name}` |
| `POST plugin/rusted/api/devices` | `POST /api/devices` |
| `DELETE plugin/rusted/api/devices/{name}` | `DELETE /api/devices/{name}` |
| `POST plugin/rusted/api/devices/{name}/backup` | `POST /api/devices/{name}/backup` |
| `GET plugin/rusted/api/devices/{name}/history` | `GET /api/devices/{name}/history` |
| `GET plugin/rusted/api/credentials` | `GET /api/credentials` |
| `POST plugin/rusted/api/credentials` | `POST /api/credentials` |
| `DELETE plugin/rusted/api/credentials/{name}` | `DELETE /api/credentials/{name}` |
| `GET plugin/rusted/api/drivers` | `GET /api/drivers` |

The receiver forwards rusted's JSON body and HTTP status, and turns any
connection failure into a clean `502` so the UI always gets JSON.

> Note: credentials are intentionally **not** managed from the web UI. Manage
> them with the `rusted cred` CLI and reference them by name when adding a
> device.
