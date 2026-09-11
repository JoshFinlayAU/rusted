# Sezione 9 — Storage in repository git

## Obiettivo

Spiega come rusted **versiona i backup** in un repository git: storage under
`./backups`, file per dispositivo, rename, remove, purge-history, e revisioni.

---

## Storage in git

### Repository

- I backup vengono archiviati in un **repository git** sotto
  `./backups` (o path `--backups` da config).
- L'opere usa il **binario system `git`** (esistente in PATH), senza
  implementazione in-process.

### File per dispositivo

- **Uno file per dispositivo**: `<group>/<device>.cfg`.
- `<group>` è il sottodirectory da `--group` o `-g` (default: `""` → `<device>.cfg`).
- Il file contiene la configurazione backup **stabilita alla differenza**
  (volatili strippati, variabili maskate).

### Storage della risposta

Per ogni backup, il contenuto `clean` viene:
1. Scritto nel file `<group>/<device>.cfg`.
2. `git add -- <relPath>` (staging).
3. **Differenza di cambiamenti** (cached diff):
   - Se **cambia**: `git commit` con message `backup <device> @ <timestamp RFC3339>`.
   - Se **non cambia**: **no commit** (usa l'HEAD corrente).
4. Il hash commit viene registrato in SQLite (`backup_runs.commit_hash`).

### Repository inizializzato

Se il repo non esiste:
- `git init`.
- Configura identità utente `user.email=rusted@localhost`, `user.name=rusted`
  in caso mancassero.
- Se non esiste `README.md` in repo, se ne crea e se commette
  `Initialise backup repository`.

---

## Rename

### `rusted device rename OLD NEW`

Rinomina un dispositivo:

- La riga store **mantiene l'ID**, quindi la storia backup (run history) resta
  associata al nome vecchio.
- Il file `<group>/OLD.cfg` viene spostato in `<group>/NEW.cfg` con `git mv` e
  commit (`Rename backup of OLD to NEW`).
- Tutte le versioni sono raggiungibili sotto il nuovo nome (git `--follow`).

### Comportamento

- **Fails** se NEW esiste (`device 'X' already exists`).
- **Fails** se NEW.cfg esiste già.
- **Fails** se OLD non esiste nel repo (non stato mai backup).
- **Fails** se git mv commette.

---

## Remove

### `rusted device remove NAME [--purge-history]`

Rimuove un dispositivo e la storia.

#### Passaggi

1. **Resolve** prima di agire: nome o path repo-relativo (`group/name.cfg`).
2. **Git** (prima):
   - `git rm -rf -q -- <relPath>` (elimina file).
   - `git commit -m "Remove backup of <relPath>"`.
   - `pruneEmptyParents` (elimina directory parent vuote).
3. **DB** (dopo): `DELETE FROM devices WHERE name = ...`.
4. Se `--purge-history`:
   - Usa `git filter-repo --path <relPath> --invert-paths --force` per esterire
     il dispositivo da **tutti** i commit.
   - Rie-mapea i hash run dei **altri** dispositivi tramite `CommitMap()` e
     `RemapRunCommits`.
   - **Irreversibile**: dopo, `git show <old-commit>:<rel>` non trova nulla.

### `--purge-history`

- **Irreversibile**.
- **Requires** `git-filter-repo` sul host (probe: `git filter-repo --version`).
- **Fails** senza `git-filter-repo`.
- Elimina la configurazione dal history git **e** dal repo forward.

### Comportamento generale

- **Fails** se device non esiste (`not found`).
- **Fails** se git agisce.
- **Fails** se DB agisce.

---

## Revisioni (commit history)

### Lista revisioni

```sh
# Via CLI (log short)
git -C <repo> log --oneline -n 20 <device>.cfg

# Via API
# GET /api/devices/<name>/versions  → lista revisioni (newest first)
```

### Revisione singola

```sh
# Via API
GET /api/devices/<name>/config?commit=<hash>
```

### Differenza tra revisioni

```sh
# Via API
GET /api/devices/<name>/diff?from=<hash>&to=<hash>
```

- `from` solo = cambiamenti introdotti da quel backup (contro il suo parent).
- `from` + `to` = diff tra due revisioni.
- `to` vuoto = HEAD.

---

## API revisioni — GET /api/devices/{name}/versions

Restituisce la storia git del file config di un dispositivo, **newest first**, con:

| Campo | Significato |
|-------|-------------|
| `commit` | Hash commit abbreviato |
| `date` | Data author, YYYY-MM-DD (short) |
| `captured_at` | Timestamp di cattura, RFC3339 con offset |
| `author` | Nome autore commit |
| `subject` | Subject commit |

Esempio JSON:

```json
[
  {
    "commit": "abc1234",
    "date": "2026-09-09",
    "captured_at": "2026-09-09T10:00:00Z",
    "author": "rusted",
    "subject": "backup nexus1 @ 2026-09-09T10:00:00Z"
  },
  {
    "commit": "def5678",
    "date": "2026-09-09",
    "captured_at": "2026-09-09T08:00:00Z",
    "author": "rusted",
    "subject": "backup core-sw1 @ 2026-09-09T08:00:00Z"
  }
]
```

---

## Download revisioni

### Per dispositivo — zip storia

```sh
# Via API
GET /api/devices/<name>/history/zip
```

Restituisce un zip con:
- `<name>/latest.cfg` — config più recente on disk.
- `<name>/<commit>.cfg` — ogni revisione.
- `<name>/MANIFEST.txt` — manifest con metadata di ogni revisione.

### Esportazione massa — zip tutta la storia

```sh
# Via API
GET /api/export/zip
```

Restituisce un zip con **tutti** i dispositivi, ogni dispositivo in una sottocartella
con `MANIFEST.txt` e revisioni. Top-level `MANIFEST.txt` indice i dispositivi.

> Nota: il download revisioni in zip è la **ricuperazione** per caso di disable
> accidental o reinstallazione. La **restituzione in-place** (re-import history nel
> repo rusted) non è supportata da `rusted` attualmente.

---

## Riepilogo

| Aspetto | Descrizione |
|---------|-------------|
| Repository | `./backups` (path `--backups` o config) |
| File | `<group>/<device>.cfg` |
| Commit solo se cambia | Differenza di cambiamenti |
| Rename | `git mv` + commit, storia raggiungibile |
| Remove | Elimina forward + commit removal |
| Purge-history | `git filter-repo` esterisce history (irreversibile) |
| Revisioni | Lista/download via API e git `log` |
| Download | Zip revisioni singole/massa |
