# Sezione 15 — Reporting errori

## Obiettivo

Spiega il **formato dei messaggi di errore** di rusted: formato `FAIL`, timeout (hard
e idle), gestione paginazione `--More--`, e errori di dump troncati.

---

## Formato generale

Quando un backup **fallisce**, rusted:
1. **Pronuncia il motivo di fallimento a **stderr**** (anche senza `--verbose`).
2. **Esita non-0**, per che schedularesti/sensi percevano il fallimento.

### Esempio di messaggio di errore

```
FAIL core-sw1: connect: dial tcp 10.0.0.1:23: connection refused

DEVICE   STATUS   COMMIT   DETAIL
core-sw1 failed    -         connect: dial tcp 10.0.0.1:23: connection refused
```

| Formato | Significato |
|---------|-------------|
| `FAIL <device>: <motivo>` | Errore di backup per dispositivo |
| `DEVICE <name> <status> <commit> <detail>` | Riepilogo tabwriter successo/fallimento |
| `%d device(s) failed` | Conteggio dispositivi falliti (stderr) |

---

## Timeout — due tipi

Rusted riconosce i **timeout** in due modi, che producono **messaggi chiari**
`command %q timed out after %s`:

### 1. Timeout hard

- Il comando run più lungo del **`--cmd-timeout`** (default 60s).
- Risoluzione: raise la per-dispositivo `--cmd-timeout` per dispositivi lenti.

### 2. Timeout idle

- Il device non invia per **`--idle-timeout`** (default 700ms) **senza** tornare al
  prompt, indicando che il comando non è completo (es. config grande con pausi naturali
  tra burst).
- Risoluzione: raise la per-dispositivo `--idle-timeout` per device che pausa
  mid-output.

### Esempio

```
FAIL ios-1: config command "show running-config": command "show running-config" timed out after 60s
```

---

## Paginazione `--More--`

Quando il device emette **prompt pagina** (`--More--` o ` -- More --`), il trasporto
invia **spazio** per avanzare. **Non** si tratta come timeout. Funziona per SSH e telnet.

---

## Dump troncato — fails loud

Se il device (o la rete) **cancella la sessione** durante il dump — es. FortiGate HA
clusters lo fan occasionalmente sotto carico — il run viene registrato come **failed**
e **nessuna cosa viene committata**, invece di salvare una captura parziale come valido:

```
FAIL fgt-cluster: config command "show full-configuration": command "show full-configuration" did not complete: device closed the session mid-output (captured 262144 bytes)
```

> **Sicurezza**: dump troncati **fails loud** — non viene salvato come backup valido.
> Questo evita di salvare configurazioni parziali in repository git.

---

## Errore di auth Cisco enable

Quando un device Cisco IOS non è in mode privilegiato e la credenza ha password
`enable`:

```
FAIL edge-fw: init command "enable": Password: prompt but no enable password
```

> Il driver invia `enable` e rista in attesa del prompt `Password:`. Se non c'è
> password enable configurata, **fails**.

---

## Status di run

| Status | Significato | Commit |
|--------|-------------|--------|
| `success` | Backup committato (contenuto modificato) | Hash nuovo |
| `unchanged` | Nessun cambio (no diff) | Hash HEAD (non nuovo) |
| `failed` | Backup fallito | — |

---

## Progressivi (`--verbose`, `--debug`)

### `-v, --verbose`

Stampa **progressi a stderr**:

```
=== core-sw1 [10.0.0.1:cisco_ios] ===
  using transport "ssh", driver "cisco_ios"
  connected, running 3 init command(s)
  init commands ok, collecting config (1 command(s))
  captured 4240 bytes, saving to git...
```

### `-v, --debug`

Stampa **I/O device raw** (ogni comando inviato e byte ricevuti), impliece `-v`:

```
=== core-sw1 [10.0.0.1:cisco_ios] ===
  using transport "ssh", driver "cisco_ios"
  connected, running 3 init command(s)
  >>> enable
  <<< Password:
  >>> [enable password]
  <<< core-sw1#
  >>> terminal length 0
  <<< core-sw1#
  >>> show running-config
  <<< Building configuration...
  <<< version 12.2
  <<< ...
  <<< 4240 bytes captured
  captured 4240 bytes, saving to git...
```

| Prefixo | Significato |
|---------|-------------|
| `>>>` | Comando inviato al device |
| `<<<` | Risposta ricevuta dal device |
| Secreti | Mistrati (es. `[enable password]`) |

### `rusted backup history NAME`

Mostra il **messaggio completo** di ogni run passato (inclusi fallimenti), per
ricuperare il motivo di un errore passato.

---

## Riepilogo reporting errori

| Aspetto | Descrizione |
|---------|-------------|
| Errore fallback | Motivo a stderr + esitazione non-0 |
| Formato `FAIL` | `FAIL <device>: <motivo>` |
| Timeout hard | `--cmd-timeout` default 60s |
| Timeout idle | `--idle-timeout` default 700ms |
| Paginazione | `--More--` gestiona con spazio inviato |
| Dump troncato | Fails loud, non salvato |
| Auth Cisco | `Password: prompt but no enable password` |
| Progressivi | `-v` (progressi), `-vv/--debug` (I/O raw) |
