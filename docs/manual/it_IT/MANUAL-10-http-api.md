# Sezione 10 — API HTTP (rusted serve)

## Obiettivo

Spiega l'**API HTTP** di rusted: endpoint `/api/*`, autenticazione, responses,
e gestione degli errori.

---

## Avvio

```sh
rusted serve --addr :8080 --token <token>
# o
rusted serve
```

- **Token** viene usato per autenticazione Bearer su tutti gli endpoint
  `/api/*` (tranne `/healthz`).
- Se **token mancante**, l'avvio **fails** con errore
  (`an API token is required: ...`).

### Output di avvio

```
rusted API listening on :8080
```

---

## Autenticazione

- **Header**: `Authorization: Bearer <token>`.
- **Token** provienza dal file config (`api_token`) o `RUSTED_API_TOKEN`
  (variabile d'ambiente).
- Usi **authenticazione costante (constant-time)** (`crypto/subtle`), per evitare
  timing attacks.
- **`/healthz`** è aperto (senza auth).

---

## Router e middleware

Il server usa `http.ServeMux` con i handler definiti in `internal/api/api.go`,
e un **middleware auth** che valida il token per ogni endpoint `/api/*` (escluso
`/healthz`).

### Endpoint principali

| Método & path | Descrizione |
|---------------|-------------|
| `GET /healthz` | Health check (aperto) |
| `GET /api/drivers` | Lista driver |
| `GET /api/transports` | Lista trasporto registrati |
| `GET /api/credentials` | Lista credenziali (senza segreti) |
| `POST /api/credentials` | Aggiungi credenza |
| `DELETE /api/credentials/{name}` | Rimuovi credenza |
| `GET /api/devices` | Lista dispositivi (con last_backup + last_status) |
| `POST /api/devices` | Aggiungi/ri-registra dispositivo (upsert) |
| `GET /api/devices/{name}` | Dettaglio dispositivo |
| `DELETE /api/devices/{name}` | Rimuovi dispositivo (+history + file repo) |
| `GET /api/devices/{name}/history` | Storia backup (50 ultimi runs) |
| `GET /api/devices/{name}/config` | Config più recente (text/plain) |
| `GET /api/devices/{name}/versions` | Revisioni git (newest first) |
| `GET /api/devices/{name}/diff` | Diff tra revisioni (text/plain) |
| `POST /api/devices/{name}/backup` | Trigger backup ora |
| `POST /api/provision/mikrotik-ssh-key` | Provisiona chiave SSH RouterOS |

---

## Responses

### Responses JSON

- Content-Type: `application/json`.
- Tutti gli endpoint che restituiscono dati JSON restituiscono JSON.
- Errori restituiscono JSON con `{"error": "<messaggio>"}` e status appropriato.

### Responses text/plain

- Endpoint che restituiscono **testo raw** (config, diff):
  - `GET /api/devices/{name}/config` — config testo.
  - `GET /api/devices/{name}/diff` — diff testo.
  - Content-Type: `text/plain; charset=utf-8`.
- **Transport failure** si convertisce in **502** con testo.

### Responses di successo esempi

**Lista dispositivi** (`GET /api/devices`):

```json
[
  {
    "name": "nexus1",
    "host": "10.0.0.1",
    "port": 22,
    "driver": "cisco_nxos",
    "transport": "",
    "credential": "lab",
    "group": "datacenter",
    "enabled": true,
    "last_backup": "2026-09-09T10:00:00Z",
    "last_status": "success"
  }
]
```

**Dettaglio dispositivo** (`GET /api/devices/{name}`):

```json
{
  "name": "nexus1",
  "host": "10.0.0.1",
  "port": 22,
  "driver": "cisco_nxos",
  "transport": "",
  "credential": "lab",
  "group": "datacenter",
  "enabled": true
}
```

**Trigger backup** (`POST /api/devices/{name}/backup`):

```json
{
  "device": "nexus1",
  "status": "success",
  "message": "configuration updated",
  "commit": "abc1234",
  "bytes": 4240
}
```

**Storia backup** (`GET /api/devices/{name}/history`):

```json
[
  {
    "started_at": "2026-09-09T10:00:00Z",
    "finished_at": "2026-09-09T10:00:05Z",
    "status": "success",
    "message": "configuration updated",
    "bytes": 4240,
    "commit": "abc1234"
  }
]
```

**Revisioni** (`GET /api/devices/{name}/versions`):

```json
[
  {
    "commit": "abc1234",
    "date": "2026-09-09",
    "captured_at": "2026-09-09T10:00:00Z",
    "author": "rusted",
    "subject": "backup nexus1 @ 2026-09-09T10:00:00Z"
  }
]
```

**Config** (`GET /api/devices/{name}/config`):

```text
! interface GigabitEthernet0/1
  description "edge"
  switchport mode trunk
```

**Diff** (`GET /api/devices/{name}/diff?from=<hash>&to=<hash>`):

```diff
! Last configuration change at <TIMESTAMP>
+! Last configuration change at <TIMESTAMP>

! interface GigabitEthernet0/1
  description "edge"
  switchport mode trunk
```

---

## Gestione degli errori

### Errori generali

- **Auth no autorizzerio**: `401 Unauthorized` → `{"error": "unauthorized"}`.
- **Device non trovato**: `404 Not Found` → `{"error": "not found"}`.
- **Credenza non conosciuta**: `400 Bad Request` → `{"error": "unknown credential: X"}`.
- **Config JSON invallo**: `400 Bad Request` → `{"error": "invalid JSON"}`.
- **API token mancante**: avvio fails con errore.
- **Transport failure** (esempio, backup fallito): `502 Bad Gateway` → JSON con errore.
- **Purge senza git-filter-repo**: `501 Not Implemented` → `{"error": "purge requested but git-filter-repo is not installed..."}`.

### Codici HTTP di successo

| Codice | Significato |
|--------|-------------|
| `200` | Successo |
| `201` | Aggiunto/creato |
| `400` | Request invallo / errore
| `401` | No autorizzerio
| `404` | Non trovato
| `501` | Non implementato (purge senza git-filter-repo)
| `502` | Gateway fallito (transport failure)

---

## Endpoint specifici

### Lista dispositivi con status

`GET /api/devices`

- Restituisce ogni dispositivo con `last_backup` (timestamp ultimo run, o `null` se
  nessuno) e `last_status` (`"success"`, `"unchanged"`, `"failed"`, o `""` se
  nessun run).

### Device detail

`GET /api/devices/{name}`

- Dettaglio singolo dispositivo.

### Delete dispositivo

`DELETE /api/devices/{name} [?purge=true]`

- Rimuove dispositivo, la storia run e il file repo.
- **`?purge=true`**: esterisce anche la storia git (irreversibile, needs
  `git-filter-repo`). Nei caso si re-mapea i hash run dei altri dispositivi.
- **Fails** senza `git-filter-repo` se `purge` usato.
- Se transport fails, il dispositivo **non** viene eliminato (mantiene storia).

### Storia backup

`GET /api/devices/{name}/history`

- 50 ultimi run per dispositivo, JSON.

### Config revisione

`GET /api/devices/{name}/config?commit=<hash>`

- Config specifica revisione. Se `commit` vuoto, config più recente.
- Content-Type: `text/plain`.

### Revisioni git

`GET /api/devices/{name}/versions`

- Revisioni git (newest first), come descritto prima.

### Diff revisioni

`GET /api/devices/{name}/diff?from=<hash>&to=<hash>`

- Diff unificato tra revisioni. Content-Type: `text/plain`.
- `from` vuoto = cambiazione da quel backup (contro parent).
- `to` vuoto = HEAD.

### Trigger backup

`POST /api/devices/{name}/backup`

- Trigger backup ora con timeout 2 minuti.
- Response: JSON con Result.

---

## Credenziali via API

| Endpoint | Método | Descrizione |
|----------|--------|-------------|
| `/api/credentials` | `GET` | Lista credenziali (senza segreti) |
| `/api/credentials` | `POST` | Aggiungi credenza (name, username, password, enable, private_key) |
| `/api/credentials/{name}` | `DELETE` | Rimuovi credenza |

> **Note**: le credenziali sono **intenzionali non gestite via web UI**. Gestite
> con CLI `rusted cred` e riferite per nome quando aggiungi un dispositivo.

---

## Driver/trasporto

| Endpoint | Método | Descrizione |
|----------|--------|-------------|
| `/api/drivers` | `GET` | Lista driver |
| `/api/transports` | `GET` | Lista trasporto registrati (es. `ssh`, `telnet`, `ssh-exec`) |

---

## Riepilogo endpoint API

| Path | Método | Descrizione |
|------|--------|-------------|
| `/healthz` | `GET` | Health check (aperto) |
| `/api/drivers` | `GET` | Lista driver |
| `/api/transports` | `GET` | Lista trasporto |
| `/api/credentials` | `GET`/`POST` | Credenza lista/aggiunta |
| `/api/credentials/{name}` | `DELETE` | Rimuovi credenza |
| `/api/devices` | `GET`/`POST` | Lista/aggiunta dispositivi |
| `/api/devices/{name}` | `GET`/`DELETE` | Dettaglio/delete dispositivo |
| `/api/devices/{name}/history` | `GET` | Storia backup |
| `/api/devices/{name}/config` | `GET` | Config (text/plain) |
| `/api/devices/{name}/versions` | `GET` | Revisioni |
| `/api/devices/{name}/diff` | `GET` | Diff (text/plain) |
| `/api/devices/{name}/backup` | `POST` | Trigger backup |
| `/api/provision/mikrotik-ssh-key` | `POST` | Provisiona chiave SSH RouterOS |
