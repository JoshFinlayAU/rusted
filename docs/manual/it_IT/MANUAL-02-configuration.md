# Sezione 2 — Configurazione

## Obiettivo

Spiega come configurare rusted: file `config.toml`, variabili d'ambiente,
precedenza del resolution delle impostazioni, e l'utilizzo di `rusted config`
per creare o visualizzare la configurazione.

---

## File di configurazione

Il file di configurazione è un piccolo file TOML-style (`key = value`),
modo `0600` (privato, contiene token API e secret).

### Percorsi di configurazione

Il config file viene cercato in questo ordine (quando non viene passato esplicitamente):

1. `$RUSTED_CONFIG` (variabile d'ambiente)
2. `~/.config/rusted/config.toml` (utente, rispettando `XDG_CONFIG_HOME`)
3. `/etc/rusted/config.toml` (sistema)

Puoi sovrascrivere il percorso con l'argomento `--config` (persistent flag) o
con la variabile d'ambiente `RUSTED_CONFIG`.

### Formato del file `config.toml`

```toml
# rusted configuration
# Keep this file private: it contains the API token and encryption secret.

db        = "/var/lib/rusted/rusted.db"
backups   = "/var/lib/rusted/backups"
api_addr  = ":8080"
api_token = "…"   # bearer token per l'API HTTP / LibreNMS
secret    = "…"   # chiave AES-256-GCM per l'encryption-at-rest delle credenziali
```

### Variabili d'ambiente equivalenti

Ogni chiave nel file config ha un equivalente variabile d'ambiente:

| Chiave config | Variabile d'ambiente |
|---------------|----------------------|
| `db` | `RUSTED_DB` |
| `backups` | `RUSTED_BACKUPS` |
| `api_addr` | `RUSTED_API_ADDR` |
| `api_token` | `RUSTED_API_TOKEN` |
| `secret` | `RUSTED_SECRET` |

### Esempio di configurazione completa

```toml
db        = "/var/lib/rusted/rusted.db"
backups   = "/var/lib/rusted/backups"
api_addr  = ":8080"
api_token = "a-very-long-random-token"
secret    = "a-very-long-random-secret-key"
```

---

## Resolutione delle impostazioni

Le impostazioni vengono risolse in un ordine specifico (l'ultima prevale):

```
defaults predefiniti  <  file config  <  variabili d'ambiente  <  flag CLI
```

- **Defaults predefiniti**: DB `rusted.db`, Backups `backups`, APIAddr `:8080`.
- **File config**: impostazioni dal file `config.toml`.
- **Variabili d'ambiente**: `RUSTED_*` override.
- **Flag CLI**: argomenti `--db`, `--backups`, `--config` override tutto.

Questo ordine permette di gestire le impostazioni in più punti senza sovrascrivere
delle precedenti.

### Esempio: precedenza

1. Default `db = "rusted.db"`.
2. Config file `db = "/data/rusted.db"`.
3. Variabile d'ambiente `RUSTED_DB = "/elsewhere/rusted.db"`.
4. Flag CLI `--db=/ulteriore/db`.

Il risultato finale è `/ulteriore/db` (flag CLI prevale tutto).

---

## Gestore config `rusted config`

### `rusted config init`

Crea o inizia il file di configurazione.

```sh
# User-level (default)
rusted config init

# System-wide (usa sudo per installazione globale)
rusted config init --global
```

**Effetto**: genera un file `config.toml` con un token API random e un secret di
encryption (AES-256-GCM) generati. Il file viene creato solo se non esiste.

### `rusted config show`

Visualizza le impostazioni risolte (con i segreti mastrati).

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

> I segreti (token e secret) vengono mostrati mastrati (`…`), per sicurezza.

### Modifica di una configuzione

La configurazione può essere modificata direttamente nel file
`config.toml`. Esempio:

```toml
# config.toml
db        = "/var/lib/rusted/rusted.db"
backups   = "/var/lib/rusted/backups"
api_addr  = ":8080"
api_token = "nuovo-token"
secret    = "nuovo-secret-key"
```

> **Attenzione**: il token API e il secret devono essere **stabili** per la vita
> della base. L'installer generate uno solo e non lo rota. Se modificano, è
> necessario re-criare la config per generare nuovi valori, oppure usare
> `rusted serve --addr :8080 --token <nuovo_token>` per avviare con lo token
> aggiornato senza cambiare il file config.

---

## Variabile globale `RUSTED_CONFIG`

La variabile `RUSTED_CONFIG` specifica il percorso del file config da usare. Se
definisci questa variabile, rusted la usa prima che cerchi i default
`RUSTED_CONFIG` → `~/.config/rusted/config.toml` → `/etc/rusted/config.toml`.

```sh
# Usa questa config file specifica
export RUSTED_CONFIG=/custom/path/config.toml

# O direttamente
rusted --config /custom/path/config.toml backup run --all
```

---

## Configurazione via CLI globale

Gli argomenti persistenti `--config`, `--db`, `--backups` override le configurazioni
dalla config file e dalle variabili d'ambiente:

```sh
# Sovrascrive il file config
rusted --config /etc/rusted/config.toml backup run --all

# Sovrascrive il DB path
rusted --db /custom/db.rusted.db backup run --all

# Sovrascrive il repo backup
rusted --backups /custom/backups rusted backup run --all
```

---

## Riepilogo configurazione

| Aspetto | Valore default | Punto di configurazione |
|---------|----------------|--------------------------|
| DB | `rusted.db` | Config file / `RUSTED_DB` / `--db` |
| Repo backup | `backups` | Config file / `RUSTED_BACKUPS` / `--backups` |
| API addr | `:8080` | Config file / `RUSTED_API_ADDR` / `--addr` (serve) |
| API token | (necessario) | Config file / `RUSTED_API_TOKEN` / `--token` (serve) |
| Secret (encryption) | (opzionale) | Config file / `RUSTED_SECRET` / `RUSTED_SECRET` |
| Config percorso | `$RUSTED_CONFIG` → default | `RUSTED_CONFIG` / `--config` |
