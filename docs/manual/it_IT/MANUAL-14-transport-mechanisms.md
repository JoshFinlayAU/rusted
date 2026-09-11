# Sezione 14 — Meccanismi di trasporto

## Obiettivo

Spiega i **meccanismi di trasporto** di rusted: SSH (built-in), telnet, ssh-exec,
l'interface pluggabile, e una guida per scrivere un nuovo transport module.

---

## Trasporto

Il **trasporto** è il meccanismo con cui rusted **riperce un dispositivo** e
**scambia comandi** con esso. L'SSH è built-in; altri trasporto (telnet, server console
serial, endpoint REST, NETCONF, API cloud vendor) possono essere aggiunti implementando
una piccola interfaccia e registrandola.

### Relazione trasporto-driver

- Il **driver** sa **cosa** digitar (es. `show running-config`, `/export terse`).
- Il **trasporto** sa **come** consegnare quegli keystrokes e leggere la risposta.
- Qualsiasi driver funziona su qualunque trasporto.

---

## Trasporto built-in

| Nome | Descrizione |
|------|-------------|
| `ssh` | SSH interactive shell (built-in). Richiede PTY, è come la maggioranza dei sistemi di rete diutomatisati. |
| `telnet` | Telnet con login, prompt detetto (non built-in, ma aggiungibile). |
| `ssh-exec` | SSH su channel esecuzione (no PTY). Uti per platforme dove il console interactive è inaffidabile a script. |

### Esempio di trasporto registrato

```sh
rusted driver list
rusted serve --addr :8080
# GET /api/transports  → ["ssh", "telnet", "ssh-exec", ...]
```

---

## Interface trasporto

Dal modulo `internal/transport/transport.go`:

### `Session` interface

```go
type Session interface {
    // SendCommand scrive cmd al device, aspetta che completi, e restituisce
    // il output con il comando echo e il prompt finale rimossi.
    SendCommand(cmd string) (string, error)
    // Close chiude la sessione e rilascia risorse.
    Close() error
}
```

### `Transport` interface

```go
type Transport interface {
    // Name è l'identità unica per selezionarlo, es. "telnet".
    Name() string
    // Dial opera una Sessione al target. L'chiudere è del chiamante.
    Dial(ctx context.Context, t Target) (Session, error)
}
```

### `Target` (carica info di connessione e autenticazione)

```go
type Target struct {
    Name       string
    Host       string
    Port       int
    Username   string
    Password   string
    PrivateKey []byte        // optional PEM key
    Enable     string        // optional password mode privilegiato
    Timeout    time.Duration
    CmdTimeout time.Duration
    IdleTimeout time.Duration
    Debug      io.Writer
    RawOutput  bool
}
```

---

## Come aggiungere un trasporto

1. **Crea un file** in `internal/transport/` (es. `telnet.go`), `package transport`.
2. **Implement `Transport`**:
   - `Name()`: string ID unico.
   - `Dial(ctx, t)`: apre connessione, fa login, restituisce una `Session`.
     - Mantieni stato per connessione (socketti, buffer, prompt) nel struct sessione.
3. **Implement `Session`**:
   - `SendCommand(cmd)`: scrive il comando, legge fino a che il device finisce.
     - La SSH built-in legge fino al prompt o a quando il stream va in idlesimo ~700ms.
     - Reuse quella strategia.
4. **Registra**:
   ```go
   func init() { Register(&Telnet{}) }
   ```
5. **Seleziona**: l'engine backup chiama per trasporto per nome (`Engine.Transport`,
   default `"ssh"`).

### Skeleton minimal

```go
package transport

import "context"

func init() { Register(&Telnet{}) }

type Telnet struct{}

func (*Telnet) Name() string { return "telnet" }

func (*Telnet) Dial(ctx context.Context, t Target) (Session, error) {
    // dial t.Host:t.Port, invia t.Username / t.Password, detect prompt...
    return &telnetSession{ /* ... */ }, nil
}

type telnetSession struct{ /* conn, reader, prompt ... */ }

func (s *telnetSession) SendCommand(cmd string) (string, error) {
    // scrivi cmd + "\n", legge fino a prompt o idlesimo, strappa echo + prompt
    return "", nil
}

func (s *telnetSession) Close() error { return nil }
```

---

## Esempi di strategie per trasporto

### Stampi output raw (RawOutput)

Per driver che devono catturar output **byte-exact** (es. OpenWrt con binari),
il trasporto deve:
- **Non normalizzare** l'output CRLF, ANSI.
- Rimuover la normalizzazione (es. `cleanOutput`/`ansiRe`) quando `RawOutput` è
  true.
- Rispetare `ctx` per la fase connessione, per non fermare `--all` su device
  inatto.

### Mode privilegiato (enable)

- Se una piattaforma ha bisogno di un passo `enable`, preferire farlo da **Init**
  driver; usare `Target.Enable` solo quando il trasporto stesso deve gestire un
  prompt `enable` interattivo.
- La SSH built-in risponde automaticamente al prompt `Password:` dopo `enable`
  (supporto built-in).

---

## Nota sui trasporto esistenti

### SSH (`ssh`)

- Richiede **PTY** (`RequestPty("vt100")`).
- Drives device attraverso la CLI, come la maggioranza dei sistemi di rete.
- **Prompt detetto**: prompt tipico a fine buffer (hostname-like + `>`, `#`, `$`),
  con anchoring `trailingMatches` (prompt senza riga terminante).
- **Paginazione `--More--`**: quando emette prompt pagina, il trasporto invia spazio
  per avanzare senza trattarlo come timeout. Funziona per SSH e telnet.
- **ANSI/VT100 escapes**: strizzoni (es. per MikroTik RouterOS colorisce).
- **Auth**: password + KeyboardInteractive, o private key, oppure key+password.
- **KEX/cipher**: widen per supportare hardware vecchio (legacy KEX, ciphers).
- **Host key**: `ssh.InsecureIgnoreHostKey()` — accetta key host qualsiasi.
  Pinned `known_hosts` è un piano futuro (README Roadmap).
- **Timeout**:
  - `CmdTimeout` (default 60s): timeout hard per comando.
  - `IdleTimeout` (default 700ms): window idlesimo per considerare output completo.

### ssh-exec

- Channel esecuzione (no PTY).
- Uti per OpenWrt e MikroTik (console interactive inaffidabile a script).
- Output byte-exact (no normalizzazione), adatto a post-processing driver.

---

## Riepilogo

| Trasporto | Description | Built-in |
|-----------|-------------|----------|
| `ssh` | SSH interactive shell (PTY) | ✅ Yes |
| `telnet` | Telnet con login, prompt detetto | ❌ Aggiungibile |
| `ssh-exec` | SSH channel esecuzione (no PTY) | ❌ Aggiungibile |

> Per trasporto non built-in, seguire la guida di **Sezione 14 (Meccanismi di trasporto)**
> per implementare l'interface `Transport`/`Session` e registrare.
