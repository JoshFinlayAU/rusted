# Sezione 3 — Riassunto comandi CLI

## Obiettivo

Fornisce una tabella completa di tutti i comandi `rusted` con i loro scopi e
il riassunto dei flag principali.

---

## Comandi principali

| Comando | Alias | Scopo |
|---------|-------|-------|
| `rusted init` | — | Inizializza la base database SQLite e il repository git di backup |
| `rusted config` | — | Gestisce la file di configurazione (init/show) |
| `rusted cred` | `credential`, `creds` | Gestisce le credenziali di login |
| `rusted device` | `dev`, `devices` | Gestisce l'inventario dei dispositivi di rete |
| `rusted driver` | `drivers` | Intersecazione con le driver piattaforme |
| `rusted backup` | — | Esegue backup e visualizza la storia |
| `rusted serve` | — | Avvia l'API HTTP per l'integrazione LibreNMS |

---

## Comando `rusted` (root)

| Aspetto | Valore |
|---------|--------|
| **Use** | `rusted` |
| **Short** | Network device configuration backup tool (RANCID/Oxidized replacement) |
| **Long** | rusted backs up network device configurations over SSH and versions them in a git repository. Credentials and devices are stored in SQLite. |

### Flag persistenti (applicati a tutti i comandi)

| Argomento | Descrizione |
|-----------|-------------|
| `--config <path>` | Percorso del file config (default: `$RUSTED_CONFIG`, `~/.config/rusted/config.toml`, `/etc/rusted/config.toml`) |
| `--db <path>` | Percorso della base SQLite (default: `rusted.db` o config) |
| `--backups <path>` | Percorso del repository git di backup (default: `backups` o config) |

---

## Comando `rusted init`

- **Use**: `init`
- **Short**: Inizializza la base database e il repository git di backup.
- **Effetto**: crea la base SQLite (se non esiste), il repository git di backup (se non esiste), e configura i impostamenti base.

**Output tipico**:
```
database:  /var/lib/rusted/rusted.db
backups:   /var/lib/rusted/backups
config:    /etc/rusted/config.toml
encryption: enabled
rusted initialised.
```

---

## Comando `rusted config`

- **Use**: `config`
- **Short**: Gestisce la file di configurazione.

### Comandi di sottocomando

| Comando | Use | Scopo |
|---------|-----|-------|
| `rusted config init` | `init` | Crea o inizia il file `config.toml` con token API e secret generati |
| `rusted config show` | `show` | Visualizza le impostazioni risolte (segreti mastrati) |

**Flag**: —

---

## Comando `rusted cred`

- **Use**: `cred`
- **Alias**: `credential`, `creds`
- **Short**: Gestisce le credenziali di login.

### Comandi di sottocomando

| Comando | Use | Scopo |
|---------|-----|-------|
| `rusted cred add NAME` | `add` | Aggiunge una credenziale (username, password, enable, optional key) |
| `rusted cred list` | `list` | Lista le credenziali (segreti mastrati) |
| `rusted cred remove NAME` | `remove`, `rm`, `delete` | Rimuove una credenziale (fails se usata da dispositivi) |

**Flag principali**:

| Flag | Descrizione |
|------|-------------|
| `-u, --username <u>` | Login username (**obbligatorio** per `add`) |
| `-p, --password <p>` | Login password |
| `-e, --enable <e>` | Enable/privileged password (opzionale) |
| `-k, --key <file>` | Percorso del file PEM private key (opzionale) |

---

## Comando `rusted device`

- **Use**: `device`
- **Alias**: `dev`, `devices`
- **Short**: Gestisce l'inventario dei dispositivi di rete.

### Comandi di sottocomando

| Comando | Use | Scopo |
|---------|-----|-------|
| `rusted device add NAME` | `add` | Aggiunge un dispositivo (host, driver, credential, port, timeout, etc.) |
| `rusted device list` | `list` | Lista i dispositivi con impostazioni e status ultimo backup |
| `rusted device update NAME` | `update` | Modifica i campi di un dispositivo |
| `rusted device rename OLD NEW` | `rename`, `mv`, `move` | Rinomina un dispositivo, conservando la storia |
| `rusted device remove NAME` | `remove`, `rm`, `delete` | Rimuove un dispositivo e la storia |
| `rusted device enable NAME` | — | Mark un dispositivo come **attivo** |
| `rusted device disable NAME` | — | Mark un dispositivo come **inattivato** |

**Flag principali per `add`**:

| Flag | Descrizione |
|------|-------------|
| `-H, --host <H>` | Hostname o IP (default: nome del dispositivo) |
| `-P, --port <P>` | Porta SSH (default: 22) |
| `-d, --driver <d>` | Driver piattaforma (default: `generic`) |
| `-t, --transport <t>` | Nome trasporto (default: SSH engine default) |
| `-c, --credential <c>` | Nome credenziale (**obbligatorio** per `add`) |
| `-g, --group <g>` | Sottodirectory nel repo backup |
| `--disabled` | Aggiunge il dispositivo inattivato |
| `--cmd-timeout <s>` | Timeout per comando in secondi (default: 60) |
| `--idle-timeout <ms>` | Timeout idle in millisecondi (default: 700) |

**Flag principali per `update`**: `--host`, `--port`, `--driver`, `--transport`,
`--credential`, `--group`, `--cmd-timeout`, `--idle-timeout`

**Flag per `enable`/`disable`**: `NAME` (obbligatorio)

---

## Comando `rusted driver`

- **Use**: `driver`
- **Alias**: `drivers`
- **Short**: Intersecazione con le driver piattaforme.

### Comandi di sottocomando

| Comando | Use | Scopo |
|---------|-----|-------|
| `rusted driver list` | `list` | Lista le driver disponibili (nome, descrizione, comandi di config) |

**Output tipico** (tabwriter):
```
NAME        DESCRIPTION           CONFIG COMMANDS
cisco_nxos  Cisco NX-OS          show running-config
mikrotik_routeros  MikroTik RouterOS v7+   /export terse
juniper_junos  Juniper Junos        show configuration | display set
...
```

---

## Comando `rusted backup`

- **Use**: `backup`
- **Short**: Esegue backup e visualizza la storia.

### Comandi di sottocomando

| Comando | Use | Scopo |
|---------|-----|-------|
| `rusted backup run [NAME]` | `run` | Esegue backup di un dispositivo o di tutti i dispositivi attivi (`--all`) |
| `rusted backup history NAME` | `history` | Visualizza la storia di backup per un dispositivo |

**Flag per `run`**:

| Flag | Descrizione |
|------|-------------|
| `--all` | Backup di tutti i dispositivi attivi |
| `--raw` | Salva la configurazione verbatim (esclude stripping volatili e maschera) |
| `-v, --verbose` | Stampa progressi a stderr |
| `-v`, `--debug` | Stampa I/O device raw a stderr (impliece -v) |

**Flag per `history`**:

| Flag | Descrizione |
|------|-------------|
| `-n, --limit <n>` | Max righe da visualizzare (default: 20; 0 = tutto) |

---

## Comando `rusted serve`

- **Use**: `serve`
- **Short**: Avvia l'API HTTP usato dall'integrazione LibreNMS.

**Flag principali**:

| Flag | Descrizione |
|------|-------------|
| `--addr <addr>` | Indirizzo di ascolto (default: da config o `:8080`) |
| `--token <token>` | Bearer token (default: da config o `RUSTED_API_TOKEN`) |

**Errore di avvio** (se token mancante):
```
error: an API token is required: set it in the config file, --token, or RUSTED_API_TOKEN
```

---

## Riepilogo comandi快捷 (quick reference)

```sh
# Initializzazione
rusted init
rusted config init
rusted config show

# Comandi base
rusted driver list
rusted cred add/list/remove
rusted device add/list/update/rename/remove/enable/disable

# Backup e history
rusted backup run [NAME] [--all]
rusted backup history NAME

# API
rusted serve
```
