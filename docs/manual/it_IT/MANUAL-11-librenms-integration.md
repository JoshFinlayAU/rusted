# Sezione 11 — Integrazione con LibreNMS

## Obiettivo

Spiega l'integrazione di rusted con **LibreNMS**: come installare il plugin,
come usare la UI, gli endpoint del plugin, e il troubleshooting.

---

## Spiegazione

Il plugin **rusted** integra i backup configurazioni rusted nella UI di LibreNMS
usando il **sistema di plugin v2** di LibreNMS. La pagina del plugin fa **calls AJAX
sostanzone-origin** alle rotte del plugin, che **relieggiano server-side** agli
endpoint API di rusted. La token Bearer rimane nel `.env` di LibreNMS e **non viene
invece al browser**.

---

## Requisiti

- **LibreNMS** con il sistema di plugin v2 (`app/Plugins`, il manager plugin moderno).
- **Un instante `rusted serve`** raggiungibile dal server LibreNMS, e il token API
  corrispondente.

---

## Setup rusted serve

Prima di installare il plugin, avvia un instante di `rusted serve` raggiungibile
dalla server LibreNMS. Può ascoltare solo su loopback:

```sh
# rusted serve con addr loopback (solo LibreNMS deve raggiungere)
rusted serve --addr 127.0.0.1:8080

# O con token specifico
rusted serve --addr 127.0.0.1:8080 --token <nuovo-token>
```

> Il token di `rusted serve` deve corrispondere al token `api_token` nel file config
> o alla variabile `RUSTED_API_TOKEN` di rusted.

---

## Installazione del plugin

### Opzione A — da package Composer pubblicato/VCS

```sh
# sul server LibreNMS
./lnms plugin:add athena-networks/rusted-librenms
```

### Opzione B — package path locale (development / air-gapped) — **recommandata**

Se il source del plugin è su disco sul server LibreNMS (es. `/opt/rusted-plugin/librenms-module`,
che contiene il `composer.json` del plugin):

1. **Punta una repository Composer path al directory del plugin.** Aggiungi un'entry
   `repositories` all'`composer.json` di LibreNMS. Il path deve essere **assoluto** e
   leggibile dall'utente `librenms`:

   ```sh
   jq '.repositories = [{"type":"path","path":"/opt/rusted-plugin/librenms-module"}]' \
       composer.json > composer.json.tmp && mv composer.json.tmp composer.json
   ```

   O manualmente:

   ```json
   "repositories": [
     { "type": "path", "path": "/opt/rusted-plugin/librenms-module/" }
   ]
   ```

   > **Note**:
   > - Il directory deve terminare alla cartella che contiene il `composer.json`
   >   del plugin (la cartella `librenms-module`, non il padre).
   > - Un entry scritta direttamente in `composer.json` **non sopravvive** alla
   >   auto-update notturna di LibreNMS (la `daily.sh` restore `composer.json` dal
   >   git).

2. **Installa la package.** Passare una constraint di versione:

   ```sh
   sudo -u librenms ./lnms plugin:add athena-networks/rusted-librenms @dev
   ```

   `./lnms plugin:add` invoca il wrapper Composer bundled di LibreNMS, quindi
   **non** serve il `composer` globale.

3. **Abilita il plugin.** Il plugin si registra con il nome **`rusted`**:

   ```sh
   sudo -u librenms ./lnms plugin:enable rusted
   ```

   Se installato con `plugin:add`, è già registrato in `composer.plugins.json` e può
   printing "Plugin already enabled".

4. **C清除 cache** per che le rotte, view e config pratici pratici pratici:

   ```sh
   sudo -u librenms ./lnms config:clear
   sudo -u librenms php artisan view:clear
   sudo -u librenms php artisan route:clear
   ```

5. **Aggiungi i settings di connessione** all'`composer.json` di LibreNMS `.env`:

   ```ini
   RUSTED_API_URL=http://127.0.0.1:8080
   RUSTED_API_TOKEN=a-long-random-token
   ```

### Verifica dell'installazione

Non esiste `lnms plugin:list`. Verifica che il plugin è registrato e abilitato
direttamente nella database:

```sh
sudo -u librenms php lnms db:query \
  "SELECT plugin_name, plugin_active, version FROM plugins WHERE plugin_name LIKE 'rusted%';"
```

`plugin_active` dovrebbe essere `1`.

---

## Funzionalità della UI

### Pagina Rusted Backups

- **Pagina principale** (`plugin/rusted`): interfaccia di accesso.
- **Menu entry**: "Rusted Backups" in menu principale.

### Pagina Devices

- Lista di **tutti i dispositivi** rusted conosce.
- Ogni riga è **direttamente editabile**: clicca su **Driver**, **Transport**, o
  **Port** per modificare; la modifica viene salvata immediatamente (debounced 500 ms
  per input testo).
- **Column Backup** colorato per lo stato ultimo backup:

| Icono | Significato |
|-------|-------------|
| ✔ **UP TO DATE** (verde) | Ultimo run `success` o `unchanged`, entro 24h |
| ? (grigio) | `success` o `unchanged`, ma oltre 24h (stale) |
| ⚠ **failed** (rosso) | Ultimo run fallito (tooltip con messaggio) |
| ○ **never** (grigio) | Nessun backup mai registrato |

- **Column Actions**: toggle enabled/disabled, backup, history, delete.

### Quadro di controlli dispositivo

Per ogni dispositivo, la **Overview page** di LibreNMS mostra un **Configuration
Backups** panel:

- **Enabled toggle (🔌)**: enable/disable (soft — non elimina; conserva storia).
- **Back up now (🔽)**: trigger backup ora; mostra errore se fallisce.
- **Show history (📜)**: apre view configuration (revisioni + config/diff).
- **Remove (🗑)**: dialog con optional **Irreversible purge**; prima downloada un zip
  di tutte le revisioni, poi elimina il dispositivo da rusted e il file da repo
  (purge esterisce anche il git history).
- **Add to rusted (🔄)**: quando il dispositivo non è gestito da rusted.

### Device sync

- **Button Device sync** (destra) per aggiungere o disabilita in batch di dispositivi
  LibreNMS.
- **Bulk Add**: pane due (LibreNMS ↔ rusted) per aggiungere molti dispositivi in
  batch (un driver + una credenza per famiglia).
- **Disable**: disabilita in rusted (non elimina; conserva storia).
- **Devices orphaned** (non in LibreNMS) si visualizzano grigie e bloccati.
- **Bulk export**: download di tutti i dispositivi storia in un zip.

### Download history revisioni

- Su ogni pagina device viewer, **All revisions** downloada un zip di **tutti i
  git revisioni** (`<name>/<commit>.cfg`) + `latest.cfg`.

---

## Endpoints del plugin

La pagina si chiama `GET plugin/rusted`. Le AJAX calls del browser usano rotte
sostanzone-origin del plugin. Il controller **relieggia server-side** agli endpoint
API di rusted.

| Método & path | Relieggia a rusted |
|---------------|---------------------|
| `GET plugin/rusted` | — |
| `GET plugin/rusted/device/{name}` | — |
| `GET plugin/rusted/sync` | — |
| `GET plugin/rusted/api/devices` | `GET /api/devices` |
| `GET plugin/rusted/api/devices/{name}` | `GET /api/devices/{name}` |
| `POST plugin/rusted/api/devices` | `POST /api/devices` (crea) |
| `PUT plugin/rusted/api/devices/{name}` | partiale update (GET merge, POST upsert) |
| `DELETE plugin/rusted/api/devices/{name}` | `DELETE /api/devices/{name}` (`?purge=true` per purge) |
| `POST plugin/rusted/api/devices/{name}/enabled` | flip `enabled` (preserva storia) |
| `POST plugin/rusted/api/devices/{name}/backup` | `POST /api/devices/{name}/backup` |
| `GET plugin/rusted/api/devices/{name}/history` | `GET /api/devices/{name}/history` |
| `GET plugin/rusted/api/devices/{name}/history/zip` | `GET` revisioni in zip |
| `GET plugin/rusted/api/export/zip` | bulk export tutti i dispositivi storia in zip |
| `GET plugin/rusted/api/devices/{name}/versions` | `GET /api/devices/{name}/versions` |
| `GET plugin/rusted/api/devices/{name}/config?commit=<hash>` | `GET /api/devices/{name}/config` |
| `GET plugin/rusted/api/devices/{name}/diff?from=&to=` | `GET /api/devices/{name}/diff` |
| `GET plugin/rusted/api/drivers` | `GET /api/drivers` |
| `GET plugin/rusted/api/transports` | `GET /api/transports` |
| `GET plugin/rusted/api/credentials` | `GET /api/credentials` |
| `POST plugin/rusted/api/credentials` | `POST /api/credentials` |
| `DELETE plugin/rusted/api/credentials/{name}` | `DELETE /api/credentials/{name}` |
| `GET plugin/rusted/api/sync/state` | join LibreNMS + rusted devices |
| `POST plugin/rusted/api/sync/add` | batch `POST /api/devices` |
| `POST plugin/rusted/api/sync/disable` | `enabled=false` per dispositivo |

### Protección

- Tutti gli endpoint `web` + `auth` + **CSRF** protetti.
- La token Bearer rimane nel `.env` di LibreNMS, non al browser.
- Transport failure si converte in **502** (JSON o testo) per che l'UI riceva
  sempre risposta JSON/testo.

---

## Layout del plugin

```
librenms-module/
├── composer.json                    # manifest package (registra provider)
├── config/config.php                # api_url / api_token / timeout (dal .env)
├── routes/web.php                    # pagina + viewer device + bulk-sync rotte
├── src/
│   ├── RustedServiceProvider.php    # registra hook, rotte, view, config
│   ├── Support/RustedClient.php     # client HTTP server-side per l'API rusted
│   ├── Controllers/RustedController.php  # pagina + viewer + bulk-sync + receiver
│   ├── Hooks/
│   │   ├── MenuEntry.php
│   │   ├── Settings.php
│   │   └── DeviceOverview.php
│   └── Views/                       # Blade (menu, pagina, device, sync, overview, settings)
└── resources/views/
```

---

## Installazione persistente tra auto-update notturne

LibreNMS auto-update se stesso una volta al giorno tramite `daily.sh` (cron). Prima
dello pull, restore i file Composer dal git, silenziosamente **cancellando** le
modificazioni locali (`daily.sh`).

Per mantenere l'installazione persistente, **registra la repository path nel
configurazione globale Composer** (`<COMPOSER_HOME>/config.json`, es.
`/opt/librenms/.composer/config.json`), che si trova **fuori** il tree git LibreNMS
e sopravvive alle reset.

```sh
sudo -u librenms php composer.phar config --global repositories.rusted \
  '{"type":"path","url":"/opt/rusted-plugin/librenms-module"}'
```

> **Note**:
> - Il URL path deve essere assoluto e leggibile dall'utente `librenms`, terminante
>   alla cartella `librenms-module` (non il padre).
> - Riepetere il comando con nome esistente **sostituisce solo** quell'entry; le
>   altre repository sono preserve.
> - `grep 'Updating Composer packages' logs/daily.log` deve terminare in `OK` dopo
>   il primo run notturno, e `vendor/athena-networks/rusted-librenms` deve essere
>   ancora presente il giorno seguente.

---

## Troubleshooting

### `The property url is required` / `type does not have a value in the enumeration ["path"]`

Alcuni build Composer hanno schema stricto per repository path che richiede la
proprietà **`$url`** invece di `$path`.

**Soluzione**: sostituisci `path` con `url` nella entry:

```json
{ "type": "path", "url": "/opt/rusted-plugin/librenms-module" }
```

Entrambi i formati comportano identicamente per repository path (Composer
symlink il directory); solo il nome proprietà varia per versione schema.

### `composer: command not found`

Usa `./lnms plugin:add ...` invece di invocare `composer` direttamente — `lnms` usa
il `composer.phar` bundled di LibreNMS via `scripts/composer_wrapper.php`, quindi
non serve l'installazione globale Composer.

### Permission denied sul path del plugin

L'utente `librenms` deve essere in grado di **read** (e stat) ogni directory nel
path al plugin:

```sh
chmod o+x /opt
chmod -R o+rX /opt/rusted-plugin
```

---

## Riepilogo

| Aspetto | Descrizione |
|---------|-------------|
| Installazione | `plugin:add` + `plugin:enable` (opzioni: Composer / path) |
| Connection | `.env`: `RUSTED_API_URL`, `RUSTED_API_TOKEN` |
| UI | Devices page (editabile, backup status), Configuration Backups panel, Device sync |
| Endpoints | 30+ rotte `plugin/rusted/*` che relieggiano a rusted API |
| Persistenza | Repository Composer global per auto-update notturne |
| Troubleshooting | Schema Composer, permission, `composer` not found |
