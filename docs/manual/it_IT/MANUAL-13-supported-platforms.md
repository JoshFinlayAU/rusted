# Sezione 13 — Piattaforme supportate

## Obiettivo

Elenca tutte le **driver piattaforme** supportate da rusted, con notes specifiche
per ogni piattaforma.

---

## Driver supportati

Rusted supporta driver per diverse piattaforme di rete. Esegua
`rusted driver list` per vedere tutto.

### Driver ufficiali e supportati

| Driver | Piattaforma | Nome config | Nota |
|--------|-------------|--------------|------|
| `cisco_nxos` | Cisco Nexus (NX-OS) | `cisco_nxos` | — |
| `mikrotik_routeros` | MikroTik RouterOS v7+ | `mikrotik_routeros` | Backup via SSH (`ssh-exec`); no API config completa |
| `juniper_junos` | Juniper Junos | `juniper_junos` | — |
| `cisco_ios` | Cisco IOS / IOS-XE | `cisco_ios` | Con supporto enable-mode automatico |
| `cisco_asa` | Cisco ASA | `cisco_asa` | Con supporto enable-mode |
| `arista_eos` | Arista EOS | `arista_eos` | — |
| `fortinet` | Fortinet FortiOS | `fortinet` | Stripping secrets re-encriptati; PEM encrypted blocks |
| `openwrt` | OpenWrt | `openwrt` | Config = filesystem; binaries base64 |
| `vyos` | VyOS / Vyatta | `vyos` | — |
| `generic` | Piattaforma generica | `generic` | Fallback; no cleanup |

### Driver nati (DRAFT) — validati contro la realtà prima

| Driver | Piattaforma | Nome config | Nota |
|--------|-------------|--------------|------|
| `cambium_epmp` | Cambium ePMP (AP/SM) | `cambium_epmp` | DRAFT; JSON export; `cfgUtcTimestamp` stripita; `RawNormalize` |
| `cambium_cnmatrix` | Cambium cnMatrix switch | `cambium_cnmatrix` | DRAFT; Cisco-like; Init paging-disable è stima |

> Non esistono driver per altre Cambium/PTP (PMP450/PTP650 usano web GUI o SNMP;
> PTP820/PTP850 esportano config in file FTP/SFTP — non supportati come SSH driver).

---

## Nota per la MikroTik RouterOS

RouterOS **non** restituisce config completa attraverso l'API:
- `/export` (API) non funziona, restituisce nulla.
- `/file` (API) cappo ~4KB.

Quindi la backup di MikroTik si fa attraverso **SSH** con il driver
`mikrotik_routeros` (transport `ssh-exec`). L'output `/export terse` emette
una linea completa per path, che diff segue molto più leggermente.

### Provisione chiave SSH per RouterOS

Se l'autenticazione SSH è un problema, puoi provvisionare una chiave SSH generata
attraverso l'API di RouterOS (`POST /api/provision/mikrotik-ssh-key`):
- Genera una **ed25519** keypair.
- Installa la **chiave pubblica** over l'API (scrive file, importa per l'utente).
- Restituisce la **chiave privata PEM** a chi la usare per la backup via SSH.
- Garantizce che il servizio SSH sia attivo (se era disattivato, lo disattiva e lo
  attiva).

```sh
# Via API (come lo invece del driver)
rusted serve --addr 127.0.0.1:8080
# POST /api/provision/mikrotik-ssh-key
# Body: {"host": "10.0.0.2", "port": 8728, "username": "admin", "password": "<pass>"}
```

> Rista **non gestisce** la provvisione della credenza da web UI. Gestite con
> `rusted cred add` e riferite per nome per il dispositivo.

---

## Nota per FortiOS

Il driver `fortinet` fa due cose speciali su ogni save:
- **Strips** `#conf_file_ver=` (contatore salva, rotante).
- **Strips** secrets re-encriptati (`set <var> ENC ...` — fresh ciphertext ogni save,
  non decodifica-volvili).
- **Strips blocks PEM encrypted** (`set private-key "-----BEGIN ENCRYPTED PRIVATE KEY-----..."`)
  — re-encriptato con fresh salt/IV ogni save, quindi base64 non corrisponde tra dumpi.

> I blocchi certificati **plain** e chiavi pubbliche sono **deterministici**, quindi
> restano nel backup.

---

## Nota per OpenWrt

OpenWrt è una distribuzione Linux: non c'è `show running-config`. La configurazione
**è** il filesystem. Il driver `openwrt`:
- Capta attraverso **ssh-exec** (byte-exact, no PTY).
- **Usa solo builtins** di `ash` (`for`/`case`/`read`/`test`/`echo`) + `cat` —
  nessun `od`, `find`, `sort`, `sed`, `head`, `base64` (non garantiti in busybox).
- Tutta la formattazione avviene da rusted (`PostProcess`).
- Files binari (con NUL byte negli primi 4KB) vengono **base64-encoded** con marker
  `## binary file, base64 follows`, mantenendo il backup testo-only, byte-faithful,
  diff-stable.
- Ordine stabile: glob alphabetico per directory, entries in ordine manifesto.

---

## Nota per Cisco IOS (enable-mode)

Il driver `cisco_ios` ha:
- **Init**: `enable`, `terminal length 0`, `terminal width 0`.
- **Config**: `show running-config`.
- **Strip**: `Building configuration...`, `Current configuration`, `! Last configuration change`, `! NVRAM config last updated`, `ntp clock-period`.

Quando una **credenza** ha una password `enable`, rusted invia `enable` e risponde
automaticamente al prompt `Password:` (supporto built-in SSH transport).

---

## Nota per Juniper Junos

Il driver `juniper_junos`:
- **Init**: `set cli screen-length 0`, `set cli screen-width 0`.
- **Config**: `show configuration | display set`.
- **Strip**: `## Last commit:`.

---

## Driver generic

Il driver `generic` è il **fallback** quando un driver non è riconosciuto. Non è
fatal, ma si nota in `rusted backup run`:

```
unknown driver 'X', used generic
```

> Se usate un driver non riconosciuto, potete usare `rusted driver list` per
> confermare i nomi corretti.

---

## Riepilogo

| Driver | Piattaforma | Supportato |
|--------|-------------|------------|
| `cisco_nxos` | Cisco Nexus NX-OS | ✅ Ufficiale |
| `mikrotik_routeros` | MikroTik RouterOS v7+ | ✅ Ufficiale (SSH) |
| `juniper_junos` | Juniper Junos | ✅ Ufficiale |
| `cisco_ios` | Cisco IOS/IOS-XE | ✅ Ufficiale (enable) |
| `cisco_asa` | Cisco ASA | ✅ Ufficiale (enable) |
| `arista_eos` | Arista EOS | ✅ Ufficiale |
| `fortinet` | Fortinet FortiOS | ✅ Ufficiale |
| `openwrt` | OpenWrt | ✅ Ufficiale |
| `vyos` | VyOS/Vyatta | ✅ Ufficiale |
| `generic` | Piattaforma generica | ✅ Fallback |
| `cambium_epmp` | Cambium ePMP | 🟡 DRAFT |
| `cambium_cnmatrix` | Cambium cnMatrix | 🟡 DRAFT |
