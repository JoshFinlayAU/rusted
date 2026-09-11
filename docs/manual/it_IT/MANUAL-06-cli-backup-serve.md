# Sezione 6 — Comandi backup/serve

## Obiettivo

Dettagli completi dei comandi `rusted backup run`, `rusted backup history` e
`rusted serve`, inclusi tutti i flag e esempi pratici.

---

## `rusted backup run [NAME]`

- **Use**: `run [NAME]`
- **Short**: Esegue backup di un dispositivo (o `--all` i dispositivi attivi).

### Argomenti

- `NAME` (opzionale): nome del dispositivo da backup. **Se non fornito e non
  usato `--all`, fails** con errore (`specify a device NAME or use --all`).

### Flag

| Flag | Descrizione |
|------|-------------|
| `--all` | Backup di tutti i dispositivi attivi |
| `--raw` | Salva configurazione verbatim (esclude stripping volatili e maschera) |
| `-v, --verbose` | Stampa progressi a stderr |
| `-v, --debug` | Stampa I/O device raw a stderr (impliece -v) |

### Esempio di utilizzo

```sh
# Backup un singolo dispositivo
rusted backup run nexus1

# Backup tutti i dispositivi attivi
rusted backup run --all

# Backup con output verbozzato
rusted backup run core-sw1 -v

# Backup verbatim (con variabili volatili incluse)
rusted backup run fgt-cluster --raw
```

### Output tipico (tabwriter per singolo device)

```
DEVICE   STATUS   COMMIT   DETAIL
nexus1   success  abc1234  backup nexus1 @ 2026-09-09T10:00:00Z

```

> `COMMIT` mostra il primo 8 caratteri del hash commit. `STATUS` è `success`,
> `unchanged`, o `failed`.

### Output tipico di successo con progressi (`-v`)

```
=== core-sw1 [10.0.0.1:cisco_ios] ===
  using transport "ssh", driver "cisco_ios"
  connected, running 3 init command(s)
  init commands ok, collecting config (1 command(s))
  captured 4240 bytes, saving to git...
DEVICE   STATUS   COMMIT   DETAIL
core-sw1 success  abc1234  backup core-sw1 @ 2026-09-09T08:00:00Z
```

### Output tipico di errore (FAIL a stderr)

```
FAIL core-sw1: connect: dial tcp 10.0.0.1:23: connection refused

DEVICE   STATUS   COMMIT   DETAIL
core-sw1 failed    -         connect: dial tcp 10.0.0.1:23: connection refused
```

#### Comportamento

- **Backup di un dispositivo**: `BackupDevice` → `backup()`.
- **Backup di tutti**: `BackupAll` → per ogni dispositivo attivo (`Enabled=true`).
- **Exits non-0 se ci sono errori**, per che schedularesti/sensi percevano il
  fallimento.
- **Record run** in SQLite `backup_runs` con status, message, bytes, commit hash.
- Se **empty capture**: status `failed` (`captured empty configuration`).
- Se **git save fails**: status `failed` (`git: <errore>`).
- Se **device senza credenza**: status `failed` (`device has no credential`).
- Se **driver non riconosciuto**: non fatal, ma note `unknown driver 'X', used generic`.

---

## `rusted backup history NAME`

- **Use**: `history NAME`
- **Short**: Visualizza la storia di backup per un dispositivo.

### Flag

| Flag | Descrizione |
|------|-------------|
| `-n, --limit <n>` | Max righe da visualizzare (default: 20; 0 = tutto) |

### Esempio

```sh
rusted backup history nexus1
rusted backup history ios-1 -n 5
```

### Output tipico (tabwriter)

```
STARTED      STATUS  BYTES  COMMIT  DETAIL
2026-09-09 10:00:00  success    4240  abc1234  backup nexus1 @ 2026-09-09T10:00:00Z
2026-09-09 08:00:00  unchanged  4240  def5678  backup core-sw1 @ 2026-09-09T08:00:00Z

```

> `STARTED` è la data timestamp del run. `COMMIT` è il primo 8 caratteri del
  hash commit. `DETAIL` è il messaggio descrittivo.

---

## `rusted serve` — API HTTP

- **Use**: `serve`
- **Short**: Avvia l'API HTTP per l'integrazione LibreNMS.

### Flag

| Flag | Descrizione |
|------|-------------|
| `--addr <addr>` | Indirizzo di ascolto (default: da config o `:8080`) |
| `--token <token>` | Bearer token (default: da config o `RUSTED_API_TOKEN`) |

### Esempio

```sh
# Usa config da file
rusted serve

# Usa addr token specifici
rusted serve --addr :8080 --token "$(openssl rand -hex 32)"

# Avvia con token da variabile d'ambiente
RUSTED_API_TOKEN=my-token rusted serve --addr 127.0.0.1:8080
```

### Output di avvio

```
rusted API listening on :8080
```

### Autenticazione

- Tutti gli endpoint `/api/*` richiedono
  `Authorization: Bearer <token>`.
- `/healthz` è aperto (senza auth).
- Se **token mancante**, l'avvio **fails** con errore
  (`an API token is required: ...`).

### Comportamento

- Avvia server HTTP con gli endpoint definiti in `internal/api/api.go`.
- Integrazione con la base SQLite (`Store`) e il repository git (`Git`) e l'
  engine backup.
- Usato dall'integrazione LibreNMS (vedi sezione LibreNMS integration).

---

## Riepilogo

| Comando | Scopo |
|---------|-------|
| `rusted backup run [NAME]` | Esegue backup |
| `rusted backup run --all` | Backup tutti i dispositivi attivi |
| `rusted backup run NAME --raw` | Backup verbatim |
| `rusted backup run -v` | Output verbozzato |
| `rusted backup run NAME --debug` | Output I/O raw |
| `rusted backup history NAME` | Storia backup |
| `rusted serve` | Avvia API HTTP |
