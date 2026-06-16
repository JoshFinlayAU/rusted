# Rusted LibreNMS plugin

A LibreNMS plugin that integrates [rusted](../README.md) configuration backups
into the LibreNMS web UI. It uses LibreNMS's **v2 plugin system** and talks to
the `rusted serve` HTTP API.

## What it provides

- A **Rusted Backups** menu entry.
- A management page to **add and remove devices**.
- **View backup history** per device (status, timestamps, commit, detail).
- **Trigger a backup on demand** with success/failure feedback.
- A **settings page** showing API connectivity status.

The LibreNMS server is the only thing that talks to the rusted API — the bearer
token stays server-side and is never exposed to the browser.

## Requirements

- LibreNMS with the v2 plugin system (`app/Plugins`, the modern plugin manager).
- A reachable `rusted serve` instance and its API token.

## Install

rusted's API must be running first:

```sh
export RUSTED_API_TOKEN="a-long-random-token"
rusted serve --addr 0.0.0.0:8080
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
routes/web.php                    plugin routes (plugin/rusted/...)
src/RustedServiceProvider.php     registers hooks, routes, views, config
src/Support/RustedClient.php      server-side HTTP client for the rusted API
src/Hooks/MenuEntry.php           menu hook
src/Hooks/Settings.php            settings hook
src/Controllers/RustedController.php  list / add / remove / backup / history
resources/views/                  menu, page, history, settings (Blade)
```

## API endpoints used

See the [rusted HTTP API](../README.md#http-api--librenms). This plugin uses:
`GET /api/devices`, `GET /api/drivers`, `POST /api/devices`,
`DELETE /api/devices/{name}`, `GET /api/devices/{name}/history`,
`POST /api/devices/{name}/backup`, and `GET /healthz`.

> Note: credentials are intentionally **not** managed from the web UI. Manage
> them with the `rusted cred` CLI and reference them by name when adding a
> device.
