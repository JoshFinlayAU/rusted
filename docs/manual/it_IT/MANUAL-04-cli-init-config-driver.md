# Sezione 4 — Comandi init/config/driver

## Obiettivo

Dettagli completi dei comandi `rusted init`, `rusted config` e `rusted driver
list`. Inclusi esempi pratici e output attesi.

---

## `rusted init`

### Scopo

Crea o inizia la base database SQLite e il repository git di backup. È il primo
passo indispensabile per funzionare rusted.

### Esempio di utilizzo

```sh
# User-level (default, usa XDG paths)
rusted init

# Con config specifica
rusted init --config /etc/rusted/config.toml

# Con DB e backup specifici
rusted init --config /custom/config.toml --db /custom/rusted.db --backups /custom/backups
```

### Output tipico

```
database:  /var/lib/rusted/rusted.db
backups:   /var/lib/rusted/backups
config:    /etc/rusted/config.toml
encryption: enabled
rusted initialised.
```

Se la encryption non è abilitata (non è stato setto un `secret`):

```
encryption: DISABLED — set a secret (config 'secret' or RUSTED_SECRET) to encrypt stored credentials
```

### Comportamento

- **Database SQLite**: crea la tabella `credentials`, `devices`, `backup_runs`
  con la migrazione aggiuntiva (per schemi precedenti).
- **Repository git**: crea il repo `./backups` (o path `--backups`), inizia con
  `git init`, configura identità utente (`rusted@localhost` / `rusted`) in caso
  mancassero, e se non esiste un `README.md` se ne crea e si commite.
- **Encryption**: se `secret` configurato (config o `RUSTED_SECRET`), l'encryption
  at-rest viene attivata.

### Verifica

```sh
# Verifica DB
ls -la /var/lib/rusted/rusted.db          # globale
# o
ls -la ~/.local/share/rusted/rusted.db    # utente

# Verifica repo backup
ls -la /var/lib/rusted/backups/           # globale
# o
ls -la ~/.local/share/rusted/backups/     # utente
```

---

## `rusted config`

### Scopo

Gestisce la file di configurazione `config.toml`.

### Comandi

#### `rusted config init`

Crea o inizia il file di configurazione con token API random e secret di encryption
generati.

```sh
# User-level
rusted config init

# System-wide
rusted config init --global

# Con posizione dati specifica
rusted config init --data-dir /var/lib/rusted
```

**Effetto**: crea un file `config.toml` privato (modo 0600) con i campi:
`db`, `backups`, `api_addr`, `api_token`, `secret`.

### `rusted config show`

Visualizza le impostazioni risolte, con segreti mastrati.

```sh
rusted config show
```

**Output tipico**:

```
# rusted configuration
# Keep this file private: it contains the API token and encryption secret.

db        = "/var/lib/rusted/rusted.db"
backups   = "/var/lib/rusted/backups"
api_addr  = ":8080"
api_token = "…"   # bearer token (masked)
secret    = "…"   # AES-256-GCM key (masked)
```

### Modifica della configurazione

Modifica direttamente il file `config.toml`:

```toml
# config.toml
db        = "/var/lib/rusted/rusted.db"
backups   = "/var/lib/rusted/backups"
api_addr  = ":8080"
api_token = "nuovo-token-random"
secret    = "nuovo-secret-random"
```

> **Importante**: il token API e il secret devono essere **stabili** per la vita
> della base. Modificare la config crea valori nuovi. Se il token API è cambiato,
> è necessario re-avviar `rusted serve` con lo nuovo token (non bisogna cambiare
> il file config se il nuovo token serve solo per `rusted serve`).

### Riepilogo flag di config

| Argomento | Descrizione |
|-----------|-------------|
| `--config <path>` | Percorso del file config (persistent) |
| `--data-dir <dir>` | Percorso dei dati (utile per installazione) |
| `--global` | Installazione globale (usa sudo) |

---

## `rusted driver list`

### Scopo

Lista le driver piattaforme disponibili con i loro nomi, descrizioni e comandi di
configurazione. Utile per confermare le piattaforme supportate e il nome del
driver da usare in comandi `device`.

### Esempio

```sh
rusted driver list
```

### Output tipico (tabwriter)

```
NAME                  DESCRIPTION                        CONFIG COMMANDS
cisco_nxos            Cisco NX-OS                         show running-config
cisco_ios             Cisco IOS / IOS-XE                  show running-config
cisco_asa             Cisco ASA                          show running-config
arista_eos            Arista EOS                         show running-config
juniper_junos         Juniper Junos                      show configuration | display set
mikrotik_routeros     MikroTik RouterOS v7+              /export terse
fortinet             Fortinet FortiOS                  show full-configuration
openwrt               OpenWrt (/etc/config files + ...)   (builtin shell commands)
vyos                 VyOS / Vyatta                      show configuration commands
generic              Unknown platform: ...               show running-config
```

### Driver nati (DRAFT)

Alcuni driver sono marcati come DRAFT e devono essere validati contro la
realtà prima di essere messi in uso:

| Driver | Piattaforma | Nota |
|--------|-------------|------|
| `cambium_epmp` | Cambium ePMP (SSH CLI, JSON export) | DRAFT; stampa JSON, con `cfgUtcTimestamp` stripita e `RawNormalize` abilitato |
| `cambium_cnmatrix` | Cambium cnMatrix switch | DRAFT; Cisco-like; l'Init paging-disable è un stima |

> **Per la MikroTik**: RouterOS non restituisce config completa attraverso l'API
> (`/export` non funziona, `/file` capped ~4KB). Quindi la backup di MikroTik si
> fa attraverso SSH con il driver `mikrotik_routeros` (transport `ssh-exec`). Se
> l'autenticazione SSH è un problema, `POST /api/provision/mikrotik-ssh-key`
> installa una chiave SSH generata over l'API e restituisce la chiave privata da usare
> per la backup (vedi sezione HTTP API e transport mechanisms).

### Driver generic

Il driver `generic` è il fallback quando un driver non è riconosciuto. Non è
fatal, ma si nota in `rusted backup run` (`unknown driver 'X', used generic`).

---

## Riepilogo

| Comando | Scopo |
|---------|-------|
| `rusted init` | Creazione DB e repo backup |
| `rusted config init` | Creazione file config con token e secret |
| `rusted config show` | Visualizza impostazioni risolte (segreti mastrati) |
| `rusted driver list` | Lista driver supportati |
