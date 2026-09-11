# Sezione 8 — Backup e differenza di cambiamenti

## Obiettivo

Spiega il meccanismo di **backup** in rusted, in particolare la **differenza di
cambiamenti** che rende i backup stabili alla differenza (un dispositivo senza
cambiamenti non genera commit spuriousi).

---

## Ciclo di backup

Per ogni backup, rusted segue questi passaggi:

1. **Raccoglie configurazione**:
   - Invia i comandi di init driver (es. `terminal length 0`) dopo il login.
   - Invia i comandi di config driver (es. `show running-config`) per
     raccoglier la configurazione.
   - La configurazione viene **mastrata** e **strippita** di volatili.

2. **Stampi il risultato**:
   - Il risultato viene scritto in `backups/<group>/<device>.cfg`.

3. **Differenza di cambiamenti** (commit solo se cambia):
   - Rusta comparesse il file con il contenuto precedente.
   - **Se cambia**: si **commit** con un hash nuovo.
   - **Se non cambia**: **no commit** (status `unchanged`), usando l'HEAD corrente.

4. **Record run** in SQLite con status, message, bytes, commit hash.

### Status di run

| Status | Significato | Commit |
|--------|-------------|--------|
| `success` | Backup committato (contenuto modificato) | Hash nuovo |
| `unchanged` | Nessun cambio (no diff) | Hash HEAD (non nuovo) |
| `failed` | Backup fallito | — |

---

## Differenza di cambiamenti — come funziona

Il meccanismo è in due layer:

### Layer 1: Stripping di linee volatili (Strip rules)

Ogni driver ha liste di regole `Strip` (regex) che **stroppa** le linee whole volatili
dalla configurazione raccolta. Esempio:

- Cisco NX-OS: stroppa `!Time:`, `!Running configuration last done at`, `!Startup config saved at`.
- Cisco IOS: stroppa `Building configuration...`, `Current configuration`, `! Last configuration change`, `! NVRAM config last updated`, `ntp clock-period`.
- MikroTik RouterOS: stroppa prompt `[user@host] >`, header `# <date> by RouterOS`, `# software id =`, `# model =`, `# serial number =`.
- FortiOS: stroppa `#conf_file_ver=`, `set <var> ENC ...`.

> Tutti i regoli `Strip` vengono applicati **per default** su ogni backup.
> `rusted backup run --raw` le **esclude** interamente.

### Layer 2: Maschera delle variabili (Normalize)

Per le linee che **vengono mantenute** (non stroppate), vengono maskate le
**variabili dynamiche** (timestamp, date, uptime) via `internal/normalize`.

Esempio:
- `! Last configuration change at 10:02:11 UTC Tue Jun 16 2026`
  → `! Last configuration change at <TIMESTAMP>`

Il rimpresstituto è un **placeholder visibile** (`<TIMESTAMP>`), non un
rimozione. Così il backup può essere letto da un umano e si vede che c'era una
timestamp, ma il valore volatile viene rimosso dal change detection.

> Questo è generico e funziona automaticamente per nuovi platforme. Solo le
> linee volatili non riconosciute dallormalizer richiedono regole `Strip`.

### Layer 3: Normalizzazione linee

Dopo il stripping e la maschera, le linee vengono:
- Trasformate `\r\n` in `\n`.
- **Trimmed** su spazi/travis.
- Assicurata una **unica riga finale** `\n`.

---

## Cambio-stabile —Perché?

Due layer (Strip + Normalize) cooperano per assicurare che **due backup di un
dispositivo senza cambiamenti producono byte identici**, il che impedisce commit
spuriousi:

| Scenario | Strip | Normalize | Risultato |
|----------|-------|-----------|-----------|
| Nessun cambiamento | linee volatili rimosse | variabili maskate | File identico |
| Cambiamento reale | — | — | File diverso → commit |

### Esempio di output diff

Per un backup con cambiamento, il commit introduce un diff:

```diff
! Last configuration change at 10:02:11 UTC Tue Jun 16 2026
+! Last configuration change at <TIMESTAMP>

! interface GigabitEthernet0/1
  description "edge"
  switchport mode trunk
```

---

## Driver specifiche — note

Alcuni driver hanno **rewrite** che vengono eseguiti su ogni save anche quando
nothing cambia, e vengono **ignorati per default**:

- **Fortinet FortiOS**: stroppa `#conf_file_ver=` (contatore salva) e secrets
  re-encriptati `set password ENC ...` (fresh ciphertext ogni save, non decodifica-
  voli, quindi stroppa i volatili). Strips blocks PEM encrypted (`set private-key
  "-----BEGIN ENCRYPTED PRIVATE KEY-----..."` — re-encriptato con fresh salt/IV ogni
  save).
- **Cambium ePMP**: driver `RawNormalize` abilitato per evitare che il masker
  generico rimuova valori JSON.

> I regoli ignorati sono **ignorati per default**. Se un platforme ha
> write su ogni save che crea commit spuriousi, il driver deve declarare i campi
> e **ignorarli**.

---

## `--raw` — backup verbatim

Per una **captura verbatim**, inclusi volatili, passare `--raw` a `rusted backup run`:

```sh
rusted backup run fgt-cluster --raw
```

### Effetto

- **Esclude** i regoli `Strip` (volatili non stroppati).
- **Esclude** la maschera Normalize (variabili non maskate).
- Solo **linee terminate** e **spazi trimmati** vengono normalizzati.
- Il contenuto salvato è **esattamente** ciò che il device emise.

### Effetto

- **Commit ogni run**: ogni backup produce un commit, anche se identico.
- Utile per audit l'esatto output del device.
- Attenzione: per un device senza cambiamenti, `--raw` produce commit spuriousi.

### Usabilità

- `rusted backup run NAME --raw`

---

## Maschera delle variabili — internal/normalize

Il modulo `internal/normalize` maskava stringhe dynamiche:

| Placeholder | Rimpresstituto |
|-------------|----------------|
| `<TIMESTAMP>` | Timestamp dynamice (cisco-style, ISO-8601, date-time verbose) |
| `<DATE>` | Data dynamiche (ISO date) |
| `<TIME>` | Orario con timezone/offset |
| `<UPTIME>` | Utenza dynamiche (`uptime is ...`, `up ...`) |

I regoli vengono applicati in ordine, più specifici (più lunghi) prima.

---

## Riepilogo

| Funzione | Descrizione |
|----------|-------------|
| Stripping volatili | Layer 1: regoli `Strip` da driver |
| Maschera variabili | Layer 2: `internal/normalize` per variabili dynamiche |
| Differenza di cambiamenti | Commit solo se contenuto diverso |
| `--raw` | Backup verbatim, esclude stripping e maschera |
| Status run | `success`/`unchanged`/`failed` |

---

## Consiglio per backup stabili

Per garantire backup stabili alla differenza:

1. **Usare il driver corretto** per la piattaforma (con regole `Strip` e
   `StripBlocks` adatti).
2. **Aprire l'encryption** (`secret` configurato) se si gestiscono credenziali sensibili.
3. **Configurare timeout adeguati** per dispositivi lenti (`--cmd-timeout`,
   `--idle-timeout`) per evitare truncation.
4. **Usare `--raw` solo per audit**, non per backup regolari.
