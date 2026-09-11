# Sezione 1 — Installazione

## Obiettivo

Spiega come installare **rusted**: tramite l'installer `install.sh` o manualmente
con `go build`. Copre i pathi predefiniti, l'installazione a livello sistema e
i passaggi post-install.

---

## Prerequisiti

Prima dell'installazione, deve essere presente:

| Requisito | Versione min. | Nota |
|-----------|---------------|------|
| **Go** | 1.26+ | Toolchain Go, per build e runtime |
| **git** | Any | Per storare i backup in repository git. L'installer emette un avviso se è assente. |

> L'installer richiede il toolchain Go e `git` per funzionare. Se `git` non è
> trovato, l'installer emette un avviso ma continua (rusted richiede `git` in
> runtime per i backup).

---

## Installazione tramite `install.sh`

Il programma includes un installer bash `install.sh`. Esecuta per installare
rusted per l'utente attivo (default).

### Opzioni

| Opzione | Effetto |
|---------|---------|
| `--user` | **Sostitutivo di default** — installa per l'utente attivo |
| `--global` | Installa a livello sistema (usa `sudo` quando non è root) |
| `--global --service` | Installa a livello sistema **e** abilita e avvia un service di sistema (systemd) |
| `-h` / `--help` | Mostra la help dell'installer |

### Exemple

```sh
# Installa per l'utente attivo (default)
./install.sh

# Installa a livello sistema
./install.sh --global

# Installa a livello sistema e abilita un service systemd (avviato dopo installazione)
./install.sh --global --service
```

### Pathi predefiniti

Il percorso dell'installer si basa su `XDG_*` se definiti, altrimenti su default:

| Aspecto | Installazione utente (default) | Installazione globale |
|---------|--------------------------------|----------------------|
| **Binary** | `~/.local/bin/rusted` | `/usr/local/bin/rusted` |
| **Config file** | `~/.config/rusted/config.toml` | `/etc/rusted/config.toml` |
| **Dati** (DB + backups) | `~/.local/share/rusted` | `/var/lib/rusted` |

> I percorsi rispettano le override `XDG_BIN_HOME`, `XDG_CONFIG_HOME`, `XDG_DATA_HOME`.
> Reeseguire l'installer non sovrascrive un config file esistente, quindi le
> credenziali critiche restano leggibili tra le versioni.

### Passaggi dell'installer

L'installer segue questi passaggi sequenziali:

1. **Verifica tooling** — verifica presenza di Go e git.
2. **Verifica privilegi** — per installazione globale, usa `sudo` se non è root.
3. **Risoluzione pathi** — stabilisce binario, config e dati.
4. **Build** — `CGO_ENABLED=0 go build -o <tmp>/rusted ./cmd/rusted`
5. **Install binary** — installa il binario con permisso 0755.
6. **Creare config** — solo se manca, genera token API random e secret di encryption
   con `rusted config init` (usa `--global` o `--data-dir` per la posizione).
7. **Inizializzare DB e repo** — `rusted init` crea la base SQLite e il repository git.
8. **Installazione service** (opzionale, solo con `--global --service`) —
   crea `rusted.service` di systemd, re-loada e abilita il timer/daemon.
9. **Hint post-install** — mostra i passaggi successivi (cred, device, backup).

---

## Installazione manuale

Per un ambiente isolato o in ambiente offline, puoi build manualmente il binario:

```sh
# Build il binario rusted (non usa l'installer)
go build -o rusted ./cmd/rusted

# Posiziona il binario sul PATH (es. per utente)
# ~/.local/bin/rusted

# In un unico comando:
CGO_ENABLED=0 go build -o ~/.local/bin/rusted ./cmd/rusted

# Inizializza la config (genera token API e secret)
rusted config init --data-dir <DIR_DEI_DATI>

# Inizializza DB e repository backup
rusted init --config <FILE_CONFIG>
```

### Configurazione dati

I pathi DB e backup possono essere passati come argomenti globali o come
variabili d'ambiente (`RUSTED_DB`, `RUSTED_BACKUPS`):

```sh
# Via config file (es. config.toml)
rusted config init --global --data-dir /var/lib/rusted

# Via variabile d'ambiente
RUSTED_DB=/var/lib/rusted/rusted.db \
RUSTED_BACKUPS=/var/lib/rusted/backups \
rusted init
```

---

## Verifica dell'installazione

Dopo l'installazione, puoi verificare che il binario e gli strumenti sono
funzionanti:

```sh
# Verifica che il binario è esistente e funziona
which rusted
rusted --help

# Verifica la config inizializzata
rusted config show

# Verifica che la DB è creata
ls -la ~/.local/share/rusted/rusted.db    # utente
# o
ls -la /var/lib/rusted/rusted.db          # globale

# Verifica che il repo backup è inizializzato
ls -la ~/.local/share/rusted/backups/     # utente
# o
ls -la /var/lib/rusted/backups/           # globale
```

---

## Post-installazioni

Dopo l'installazione, esegui questi passaggi per iniziare:

```sh
# Aggiunta credenziale reutilizzabile
rusted cred add lab -u admin -p '<password>'
#   -k ./id_ed25519   # opzionale: usare un key PEM invece/puremente del password

# Aggiunta dispositivi (driver = piattaforma; group = sottodirectory del repo)
rusted device add nexus1 -H 10.0.0.1 -d cisco_nxos -c lab -g datacenter
rusted device add edge-mt -H 10.0.0.2 -d mikrotik_routeros -c lab
rusted device add core-mx -H 10.0.0.3 -d juniper_junos -c lab

# Esecuzione del primo backup di tutti i dispositivi
rusted backup run --all
```

> Se installaste con `--service`, l'installer abilita e avvia automaticamente il
> service di sistema. Verifica con `systemctl status rusted` e
> `systemctl list-timers` se usaste `--service`.

---

## Note sull'installazione

- **Re-run installer**: non sovrascrive config file esistenti. Le credenziali
  critiche rimangono leggibili tra le versioni.
- **Service systemd**: solo supportato con `--global --service`. Verifica
  `After=network-online.target` per che i backup si eseguono dopo l'online del
  network.
- **Permissioni**: per installazione globale, i file in `/etc/`, `/var/lib/`,
  `/usr/local/bin/` devono essere accessibili. Verifica permisso con
  `ls -ld` e `chown` se necessario.
- **PATH**: se il binario non è nel PATH, aggiungilo:
  ```sh
  export PATH="$HOME/.local/bin:$PATH"
  ```
  o, per installazione globale, assicurati che `/usr/local/bin` sia nel PATH.
