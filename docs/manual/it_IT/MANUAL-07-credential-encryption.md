# Sezione 7 — Encryption-at-rest (Encryption-at-rest delle credenziali)

## Obiettivo

Spiega l'**encryption-at-rest** di rusted: come le credenziali vengono
crittografate in SQLite quando il `secret` (key di AES-256-GCM) è configurato.

---

## Conteggio

rusted fornisce **optional encryption-at-rest** per le campi sensibili delle
credenziali (password, key private, enable password) scritti nella base SQLite.

Se **non** è configurato il `secret`, i valori vengono **storati in testo** in
SQLite e rusted emette un avviso.

---

## Come funziona

### Key di master

Il key di master viene derivato dal valore di `RUSTED_SECRET` (variabile d'ambiente
o la chiave `secret` nel file config):

```go
sum := sha256.Sum256([]byte(v))   // v = RUSTED_SECRET
key = sum[:]                        // chiave AES-256
```

Il key viene usato per derivare la chiave AES-256-GCM per ogni valore sealed.

### Sealing (crittografazione)

Quando è attivata l'encryption, i valori vengono:

1. Derivati una **nonce** randomizzata (AES-256-GCM usa nonce 12 byte).
2. Crittografati con AES-256-GCM.
3. **Base64-encoded**.
4. **Prefixati** con `enc:`.

Il valore finale nel database è:
```
enc: <base64(AES-256-GCM(plaintext, key, nonce))>
```

### Opening (decrittazione)

Quando rusted legge un valore:

1. Se **non** ha prefixo `enc:`, viene restituito **inchiuso** (testo).
2. Se ha prefixo `enc:`, viene decrittato con AES-256-GCM, rimuovendo il prefixe e
   il base64.

### Schema SQLite

I valori sealed vengono scritti nel campo `password`, `private_key`, `enable`
del tabella `credentials` come stringhe prefissate `enc:`.

### Coesistenza testo/encriptato

- I **row testo** e **row encriptato** possono coesistere nel database.
- Si può attivare l'encryption più tardi, ma i row scritti mentre sono in testo
  richiedono lo **stesso secret** per essere decrittati.
- **Il secret deve essere stabile** per la vita della base — l'installer genera
  uno solo e non lo rota.

---

## Gestione del secret

### Configurazione

Il `secret` viene impostato in uno di due modi:

1. **File config** (`config.toml`):
   ```toml
   secret = "un-chiave-random-long-a-secret"
   ```

2. **Variabile d'ambiente**:
   ```sh
   export RUSTED_SECRET="un-chiave-random-long-a-secret"
   ```

### Generazione

L'installer `install.sh` genera un token API random **e** un secret di encryption
generati per la config initializzata.

```sh
rusted config init
rusted config show    # segreti mostrati mastrati, ma generati
```

### Modifiche del secret

**Non si può rotare il secret** per la vita della base:
- Modificare `config.toml` `secret` genera valori nuovi, ma i row esistenti (testo
  o encriptati) richiedono lo **stesso** secret.
- Per cambiamento effettivo, è necessario re-iniziare la base con lo stesso
  secret, oppure avviare con `--token` per il token API aggiornato (il secret
  rimane stabile).

### Ricerca

```sh
# Verifica che l'encryption sia attivata
rusted init

# O direttamente
# se secret non presente → encryption: DISABLED
# se presente → encryption: enabled
```

---

## Sicurezza

### Vantaggi

- **Credenziali non leggibili** in SQLite, anche se la base viene accessata
  direttamente.
- **AES-256-GCM**: crittografia forte con nonce randomizzato, adeguato per backup
  in repository git (non nascondono informazioni sensibili).
- **No password in database**: le credenziali vengono decrittate solo in
  memoria quando necessario per il backup.

### Ognigunche da sapere

- **Il secret è critico**. Non lo condividi in testo pubblico o in email.
- **Non rotare** il secret senza re-iniziare la base.
- **File config è privato** (modo 0600): contiene il token API e il secret.
- **Token API e secret** sono segreti: non li mostra pubblicamente.
- I segreti vengono **mistrati** nelle output CLI (`cred list`, `config show`),
  non vengono restituiti nel full.

---

## Riepilogo

| Aspetto | Descrizione |
|---------|-------------|
| Crittografia | AES-256-GCM |
| Key di master | SHA-256 di `RUSTED_SECRET` |
| Formato sealed | `enc: <base64(ciphertext)>` |
| Attivazione | `secret` config o `RUSTED_SECRET` |
| Coesistenza | Testo e encriptato possono coesistere |
| Sicurezza | Secret stabile, non rotabile senza reiniziazione |
| Segreti mastrati | Token API e secret vengono mostrati maskati |
