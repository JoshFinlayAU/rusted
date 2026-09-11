# Sezione 17 — Troubleshooting

## Obiettivo

Elenca gli **problemi comuni** di rusted e le **soluzioni** per risolverli.

---

## 1. `go: command not found` / toolchain Go non trovato

**Problema**: l'installer emette
`error: Go toolchain not found on PATH (needed to build rusted).`

**Soluzione**:
```sh
# Verifica
which go
go version

# Aggiungi Go al PATH se necessario
export PATH="$HOME/go/bin:$PATH"

# Rieduce installazione
./install.sh
```

---

## 2. `git` non trovato

**Problema**: l'installer emette avviso
`warning: 'git' not found; rusted needs it at runtime to store backups.`

**Soluzione**:
```sh
which git
git --version
# Aggiungi al PATH se necessario

# Rieduce installazione (rusted richiede git per backup)
./install.sh
```

---

## 3. `config file ... not found`

**Problema**: rusted tenta caricere config ma non trova il file
`error: config file "%q" not found`.

**Soluzione**:
- Verifica pathi config: `$RUSTED_CONFIG`, `~/.config/rusted/config.toml`,
  `/etc/rusted/config.toml`.
- Crea la config con:
  ```sh
  rusted config init
  rusted config init --global
  ```
- Passa path esplicito con `--config <path>` o variabile `RUSTED_CONFIG`.

---

## 4. `an API token is required`

**Problema**: `rusted serve` fallisce avviando perché il token API mancante
`error: an API token is required: set it in the config file, --token, or RUSTED_API_TOKEN`.

**Soluzione**:
- Imposta token nel config:
  ```toml
  api_token = "un-token-random"
  ```
- O con variabile d'ambiente:
  ```sh
  export RUSTED_API_TOKEN="un-token-random"
  ```
- O con flag serve:
  ```sh
  rusted serve --token "$(openssl rand -hex 32)"
  ```

---

## 5. Backup fallisce con `connect: ... connection refused`

**Problema**: `FAIL device: connect: dial tcp <ip>:<port>: connection refused`

**Soluzione**:
- Verifica che il device è raggiungibile sulla porta (es. SSH porta 22).
- Verifica credenza: `rusted cred list`.
- Verifica driver/trasporto: `rusted driver list`, `rusted device list`.
- Per porta diversa, usa `-P <port>` per `device add`/`update`.
- Per auth problemi, aggiorna la credenza (`rusted cred add`).

---

## 6. Backup fallisce con `Password: prompt but no enable password`

**Problema**: Cisco IOS device non in mode privilegiato, credenza senza password
`enable`.

**Soluzione**:
- Aggiungi la password `enable` alla credenza:
  ```sh
  rusted cred add lab -u admin -p '<password>' -e '<enable-password>'
  ```
- O usare un key `enable` (se disponibile).

---

## 7. Backup fallisce con timeout

**Problema**: `command "show running-config" timed out after 60s` o timeout idle.

**Soluzione**:
- Raise `--cmd-timeout` per dispositivi lenti:
  ```sh
  rusted device update slow-sw --cmd-timeout 180
  rusted device add slow-sw -H 10.0.0.5 -d cisco_ios -c lab --cmd-timeout 180
  ```
- Raise `--idle-timeout` per device che pausa mid-output:
  ```sh
  rusted device update slow-sw --idle-timeout 10000
  ```
- Rieduce `rusted device list` per vedere timeout configurati.

---

## 8. Dump troncato / sessione cancellata

**Problema**: `did not complete: device closed the session mid-output`

**Soluzione**:
- **Fails loud**: nessun backup viene salvato (security).
- Per devices che troncano sessione sotto carico (es. FortiGate HA), raise
  `--cmd-timeout` e `--idle-timeout`.
- Per devices FortiGate, il driver `fortinet` stripisce i secrets re-encriptati
  per evitare commit spuriousi.

---

## 9. Backup `unchanged` ma produce commit spuriousi

**Problema**: backup di device senza cambiamenti ma commit spuriouso.

**Soluzione**:
- Assicurati che il **driver** abbia regole `Strip` e `StripBlocks` adatte per
  la piattaforma.
- Assicurati che i **variabili dinamiche** vengano maskate (normalize).
- Per devices che re-write campi su ogni save (es. FortiGate), il driver deve
  **ignorare** i campi volatili (declarare i regoli ignori).
- Per audit verbatim, usa `--raw` ma attenzione: produce commit ogni run.

---

## 10. MikroTik backup fallisce

**Problema**: backup MikroTik non funziona, config non restituita attraverso API.

**Soluzione**:
- Backup si fa attraverso **SSH** con driver `mikrotik_routeros` (transport
  `ssh-exec`).
- Se auth SSH un problema, **provisiona chiave SSH** via API:
  ```sh
  # POST /api/provision/mikrotik-ssh-key
  # Body: {"host": "...", "port": 8728, "username": "admin", "password": "..."}
  ```
- Ruta la credenza per nome per il dispositivo (`rusted device add` con
  `-c <credenza>`).
- Verifica `rusted driver list` per confermare il driver `mikrotik_routeros`.

---

## 11. `credential ... is still used by N device(s)`

**Problema**: `rusted cred remove X` fails perché la credenza è usata.

**Soluzione**:
- Non rimuovi la credenza mentre un dispositivo la usa.
- Reassocia il dispositivo a un'altra credenza (`rusted device update <device> --credential <nuova>`).
- O mantieni la credenza.

---

## 12. `rusted device remove X --purge-history` requires git-filter-repo

**Problema**: error `--purge-history requires git-filter-repo, which is not installed`.

**Soluzione**:
- Installa `git-filter-repo`:
  ```sh
  # Debian/Ubuntu
  sudo apt install git-filter-repo
  # macOS
  brew install git-filter-repo
  # Generic
  pip install git-filter-repo
  ```
- O verifica `git filter-repo --version`.

---

## 13. `purge requested but git-filter-repo is not installed on the rusted host`

**Problema**: via API, delete device con `?purge=true` senza git-filter-repo.

**Soluzione**:
- Installa `git-filter-repo` sul host rusted (come sopra).
- O senza purge, usa delete senza `?purge=true` (elimina forward, conserva
  history).

---

## 14. Plugin LibreNMS non funziona

**Problema**: la UI LibreNMS non mostra "Rusted Backups" o non funziona.

**Soluzione**:
1. Verifica che `rusted serve` sia avviato con token corretto.
2. Verifica i settings di connessione in `.env` di LibreNMS:
   ```ini
   RUSTED_API_URL=http://127.0.0.1:8080
   RUSTED_API_TOKEN=<token>
   ```
3. Abilita il plugin: `sudo -u librenms ./lnms plugin:enable rusted`.
4. Cclear cache:
   ```sh
   sudo -u librenms ./lnms config:clear
   sudo -u librenms php artisan view:clear
   sudo -u librenms php artisan route:clear
   ```
5. Verifica con database:
   ```sh
   sudo -u librenms php lnms db:query "SELECT plugin_name, plugin_active FROM plugins WHERE plugin_name LIKE 'rusted%';"
   ```
6. Se installazione da path repository, verifica che la repository Composer
   globale sia registrata:
   ```sh
   sudo -u librenms php composer.phar config --global repositories
   ```

---

## 15. `The property url is required` / schema Composer path

**Problema**: installazione plugin fallisce con
`repositories[0].url : The property url is required` o
`repositories[0].type : Does not have a value in the enumeration ["path"]`.

**Soluzione**:
- Sostituisci `path` con `url` nella entry repository:
  ```json
  { "type": "path", "url": "/opt/rusted-plugin/librenms-module" }
  ```
- Verifica che il directory termini alla cartella `librenms-module` (non il padre).

---

## 16. Permission denied sul path del plugin

**Problema**: `Permission denied` installando plugin su path.

**Soluzione**:
```sh
chmod o+x /opt
chmod -R o+rX /opt/rusted-plugin
```

---

## 17. `composer: command not found` durante installazione plugin

**Problema**: error `composer: command not found`.

**Soluzione**:
- Usa `./lnms plugin:add ...` invece di invocare `composer` direttamente — `lnms`
  usa il `composer.phar` bundled di LibreNMS, quindi non serve l'installazione globale
  Composer.

---

## 18. Backup con `--all` fallisce solo alcuni device

**Problema**: `rusted backup run --all` esita non-0 con errori per alcuni device.

**Soluzione**:
- Verifica il output per vedere quali device fell (messaggio `FAIL <name>: ...`).
- Per device falliti, risolvere le cause specifiche (auth, timeout, connection).
- Per schedulazione, `rusted backup run --all` esita non-0 per evitare overlap e
  notificare i fallimenti.

---

## 19. Database SQLite permission

**Problema**: errori per accesso alla base SQLite.

**Soluzione**:
- Verifica permisso del database file:
  ```sh
  ls -l ~/.local/share/rusted/rusted.db
  chmod 600 ~/.local/share/rusted/rusted.db
  ```
- Per installazione globale:
  ```sh
  chmod 600 /var/lib/rusted/rusted.db
  ```
- Rieduce l'installazione se necessario.

---

## Riepilogo troubleshooting

| Problema | Soluzione chiave |
|----------|-----------------|
| Go toolchain | Aggiungi a PATH |
| git | Aggiungi a PATH |
| Config mancante | `rusted config init` |
| Token mancante | Imposta token config/env/flag |
| Connection refused | Verifica device/auth/porta |
| Timeout | Raise `--cmd-timeout`/`--idle-timeout` |
| Dump troncato | Fails loud, non salvato |
| MikroTik fallimento | Provisiona chiave SSH via API |
| Credenza usata | Non rimuovi, reassegna o mantieni |
| Purge senza git-filter-repo | Installa tool, oppure usa delete semplice |
| Plugin LibreNMS | Verifica serve, .env, abilitazione, cache |
| Schema Composer path | Sostituisci `path` con `url` |
| Permission plugin | `chmod` path |
| `composer` not found | Usa `./lnms plugin:add` |

---

## Consiglio finale

- **Documenta i dispositivi e credenziali** in tempo utile (`rusted device add`,
  `rusted cred add`).
- **Testa backup regolare** con `rusted backup run --all` prima di schedulare.
- **Monitora con `rusted device list`** lo stato ultimo backup.
- **Mantieni il database e repo accessibili** con permisso corretti.
- **Usa `--verbose`/`--debug`** per troubleshooting errori.
