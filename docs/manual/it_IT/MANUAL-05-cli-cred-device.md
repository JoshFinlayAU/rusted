# Sezione 5 — Comandi cred/device

## Obiettivo

Dettagli completi dei comandi `rusted cred` e `rusted device`, inclusi
aggiunta, lista, update, rename, remove, enable e disable, con esempi pratici
e output attesi.

---

## `rusted cred` — Gestione credenziali

- **Use**: `cred`
- **Alias**: `credential`, `creds`
- **Short**: Gestisce le credenziali di login.

### Comandi di sottocomando

| Comando | Use | Scopo |
|---------|-----|-------|
| `rusted cred add NAME` | `add` | Aggiunge una credenziale |
| `rusted cred list` | `list` | Lista le credenziali (segreti mastrati) |
| `rusted cred remove NAME` | `remove`, `rm`, `delete` | Rimuove una credenziale (fails se usata) |

---

### `rusted cred add NAME`

- **Use**: `add NAME`
- **Short**: Aggiunge una credenziale.
- **Args**: `cobra.ExactArgs(1)` — nome obbligatorio.

#### Flag

| Flag | Descrizione |
|------|-------------|
| `-u, --username <u>` | Login username (**obbligatorio**) |
| `-p, --password <p>` | Login password |
| `-e, --enable <e>` | Enable/privileged password (opzionale) |
| `-k, --key <file>` | Percorso del file PEM private key (opzionale) |

#### Esempio di utilizzo

```sh
# Aggiunta con password e password enable
rusted cred add lab -u admin -p 's3cret' -e 'enablepw'

# Aggiunta con password e key PEM (senza password)
rusted cred add lab -u admin -p '' -k ./id_ed25519

# Aggiunta con username e password e key
rusted cred add lab -u admin -p 's3cret' -e 'enablepw' -k ./id_ed25519
```

#### Output tipico

```
credential 'lab' added
```

#### Comportamento

- Upsert sulla nome unico: se la credenziale esiste, viene aggiornata in-place
  (utile per riconnettere la stessa credenza prima di ogni backup).
- Se `key` non fornito, password va usata per l'autenticazione.
- Il password, key e enable vengono **sealed con AES-256-GCM** prima di essere
  scritti in SQLite (vedi sezione Encryption-at-rest).

---

### `rusted cred list`

- **Use**: `list`
- **Short**: Lista le credenziali.

#### Output tipico (tabwriter, segreti mastrati)

```
NAME       USERNAME      PASSWORD    KEY    ENABLE
lab        admin         *********   yes    *********
lab2       user          *********   no     

```

> I segreti vengono mastrati (`********` o `-` se vuoti). Il key e enable mostrano
> solo se sono impostati (`yes`/`no`).

---

### `rusted cred remove NAME`

- **Use**: `remove NAME`
- **Alias**: `rm`, `delete`
- **Short**: Rimuove una credenziale.

#### Comportamento

- **Fails con errore** se la credenziale è usata da un dispositivo
  (`credential 'X' is still used by N device(s)`).
- Lascia in place la credenza (non cancellabile se usata).

#### Esempio

```sh
rusted cred remove lab
```

#### Output tipico

```
credential 'lab' removed
```

---

## `rusted device` — Gestione dispositivi

- **Use**: `device`
- **Alias**: `dev`, `devices`
- **Short**: Gestisce l'inventario dei dispositivi di rete.

### Comandi di sottocomando

| Comando | Use | Scopo |
|---------|-----|-------|
| `rusted device add NAME` | `add` | Aggiunge un dispositivo |
| `rusted device list` | `list` | Lista i dispositivi |
| `rusted device update NAME` | `update` | Modifica campi |
| `rusted device rename OLD NEW` | `rename`, `mv`, `move` | Rinomina, conservando storia |
| `rusted device remove NAME` | `remove`, `rm`, `delete` | Rimuove dispositivo e storia |
| `rusted device enable NAME` | — | Mark dispositivo attivo |
| `rusted device disable NAME` | — | Mark dispositivo inattivato |

---

### `rusted device add NAME`

- **Use**: `add NAME`
- **Short**: Aggiunge un dispositivo.
- **Args**: `cobra.ExactArgs(1)` — nome obbligatorio.

#### Flag

| Flag | Descrizione |
|------|-------------|
| `-H, --host <H>` | Hostname o IP (default: nome del dispositivo) |
| `-P, --port <P>` | Porta SSH (default: 22) |
| `-d, --driver <d>` | Driver piattaforma (default: `generic`) |
| `-t, --transport <t>` | Nome trasporto (default: SSH default) |
| `-c, --credential <c>` | Nome credenziale (**obbligatorio**) |
| `-g, --group <g>` | Sottodirectory nel repo backup |
| `--disabled` | Aggiunge il dispositivo inattivato |
| `--cmd-timeout <s>` | Timeout per comando in secondi (default: 60) |
| `--idle-timeout <ms>` | Timeout idle in millisecondi (default: 700) |

#### Esempio di utilizzo

```sh
# Dispositivo Cisco NX-OS
rusted device add nexus1 -H 10.0.0.1 -d cisco_nxos -c lab -g datacenter

# Dispositivo MikroTik RouterOS
rusted device add edge-mt -H 10.0.0.2 -d mikrotik_routeros -c lab

# Dispositivo Cisco IOS via telnet con timeout lento
rusted device add ios-1 -H 10.0.0.4 -d cisco_ios -c lab \
  --transport telnet -P 23 --cmd-timeout 120 --idle-timeout 2000
```

#### Output tipico

```
device 'nexus1' added
```

#### Comportamento

- **Upsert**: se il dispositivo esiste, viene aggiornato in-place
  (host, driver, transport, timeout, credential, group, enabled).
- Nome defaults al host se non specificato.
- Porta defaults a 22; driver defaults a `generic`; credential obbligatorio.
- Timeout: `--cmd-timeout` (sec) → `cmd_timeout` (0 = default 60s); `--idle-timeout`
  (ms) → `idle_timeout` (0 = default 700ms).

---

### `rusted device list`

- **Use**: `list`
- **Short**: Lista i dispositivi.

#### Output tipico (tabwriter)

```
NAME       HOST     PORT  DRIVER       TRANSPORT  CMD_TIMEOUT  IDLE_MS  GROUP  ENABLED  LAST_BACKUP  LAST_STATUS
nexus1    10.0.0.1 22     cisco_nxos   (empty)      60           -         datacenter Y  2026-09-09 10:00:00  success
edge-mt   10.0.0.2 22     mikrotik_routeros (empty) 60           -         (empty)      Y  2026-09-09 08:00:00  unchanged

```

> `TRANSPORT` è `-` se non specificato (default SSH). `ENABLED` è `Y`/`N`.
> `LAST_BACKUP` è la data timestamp del ultimo run (o `-` se nessuno).
> `LAST_STATUS` è `success`/`unchanged`/`failed`/`-` se nessun run.

---

### `rusted device update NAME`

- **Use**: `update NAME`
- **Short**: Modifica i campi di un dispositivo.

#### Flag

| Flag | Descrizione |
|------|-------------|
| `--host <H>` | Hostname o IP |
| `--port <P>` | Porta |
| `--driver <d>` | Driver piattaforma |
| `--transport <t>` | Nome trasporto (es. `ssh`, `ssh-exec`, `telnet`) |
| `--credential <c>` | Nome credenziale |
| `--group <g>` | Sottodirectory del repo |
| `--cmd-timeout <s>` | Timeout per comando in secondi (default: 60) |
| `--idle-timeout <ms>` | Timeout idle in millisecondi (default: 700) |

#### Esempio di utilizzo

```sh
# Cambia transport a telnet e timeout
rusted device update ios-1 --transport telnet --cmd-timeout 120

# Cambia driver e credential
rusted device update nexus1 --driver cisco_nxos --credential lab

# Cambia porta
rusted device update edge-mt --port 8728
```

#### Output tipico

```
device 'ios-1' updated
```

#### Comportamento

- Solo campi con flag che vengono cambiati sono modificati.
- Se nessun campo è cambiato, **fails con errore**
  (`no fields changed; use flags ...`).
- Il device non esiste → errore `not found`.

---

### `rusted device rename OLD NEW`

- **Use**: `rename NAME OLD NEW`
- **Alias**: `mv`, `move`
- **Short**: Rinomina un dispositivo, conservando la storia.

#### Scopo

Rinomina un dispositivo senza perder nulla:

- La riga store mantiene l'**ID**, quindi la storia backup (run history) resta
  associata al nome vecchio.
- Il file backup `[group/]OLD.cfg` viene spostato in `[group/]NEW.cfg` in il
  repository git con `git mv` e commit, quindi tutte le versioni sono raggiungibili
  e viewabili sotto il nuovo nome.

> Usa questa per invece di `device add NEW` + `device rm OLD`, che elimina la
> storia.

#### Comportamento

- **Fails** se il nuovo nome esiste (`device 'X' already exists`).
- **Fails** se il file target `NEW.cfg` esiste già.
- **Fails** se l'old file non esiste nel repo (dispositivo non stato mai backup).

#### Esempio

```sh
rusted device rename old-name new-name
```

#### Output tipico

```
device 'old-name' renamed to 'new-name' (history preserved)
```

---

### `rusted device remove NAME`

- **Use**: `remove NAME`
- **Alias**: `rm`, `delete`
- **Short**: Rimuove un dispositivo e la storia.

#### Flag

| Flag | Descrizione |
|------|-------------|
| `--purge-history` | Esterisce le configurazioni dal history del repo git (irreversibile, necessita `git-filter-repo`) |

#### Esempio di utilizzo

```sh
# Rimuove il dispositivo (eliminazione forward)
rusted device remove ios-1

# Esterisce anche il history del repo git
rusted device remove ios-1 --purge-history
```

#### Comportamento

- Resolve prima di agire: nome obbligatorio, oppure path repo-relativo (`group/name.cfg`).
- **Prima** viene fatto il git (rimosso file, commit removal): se fails, il
  dispositivo resta registrato.
- **Dopo** viene il DB (cancellato la riga).
- Se `--purge-history`: usa `git filter-repo --path <rel> --invert-paths` per
  esterire il dispositivo da **tutti** i commit. Nei caso si re-mapea i hash di
  run dei altri dispositivi. Requires `git-filter-repo` sul host.
- **Fails** senza `git-filter-repo` se `--purge-history` usato.

#### Output tipico

```
device 'ios-1' removed
```

> **Note**: `--purge-history` è irreversibile. Il dispositivo viene eliminato dal
> repo backup e da DB. Se serve recuperare, usa la sezione Git-versioned storage
> (download revisioni in zip).

---

### `rusted device enable NAME` / `disable NAME`

- **Use**: `enable NAME` / `disable NAME`
- **Short**: Mark un dispositivo come attivo/inattivato.

#### Esempio

```sh
rusted device enable nexus1
rusted device disable edge-mt
```

#### Output tipico

```
device 'nexus1' enabled
device 'edge-mt' disabled
```

#### Comportamento

- **Soft**: non elimina il dispositivo né la storia backup.
- `disable` stanca i backup ma conserva la storia. Il dispositivo appare
  inattivato nel repo.
- Il dispositivo si ricrea all'attivo se re-enabled.

---

## Riepilogo comandi device

| Comando | Scopo |
|---------|-------|
| `rusted device add NAME` | Aggiunge/aggiorna dispositivo |
| `rusted device list` | Lista dispositivi |
| `rusted device update NAME` | Modifica campi |
| `rusted device rename OLD NEW` | Rinomina (conserva storia) |
| `rusted device remove NAME` | Rimuove dispositivo e storia |
| `rusted device enable NAME` | Mark attivo |
| `rusted device disable NAME` | Mark inattivato |

> Nota: `device list` mostra anche lo stato `last_backup` e `last_status` per
> ogni dispositivo, utile per monitorare i backup.
