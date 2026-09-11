# Sezione 16 — Roadmap futuro

## Obiettivo

Elenca il **plan di sviluppo attuale** di rusted: feature future in arrivo.

---

## Roadmap attuale

### 1. Pinned host keys (known_hosts)

- **SSH** attualmente accetta qualunque host key (`InsecureIgnoreHostKey()`).
- **Piano**: implementare **pinned host keys** via `known_hosts` per sicurezza
  (evita acceptazione host key arbitraria).

### 2. Backup concurrente con pool di lavoro

- **`--all`** backup attualmente eseguita sequenzialmente.
- **Piano**: implementare **concurrent backup** con **worker pool** per
  accelerare `--all` su dispositivi numerosi, con controllo di risorse.

### 3. Notificazioni webhook/Slack su fallimento backup

- **Piano**: implementare **webhook**/**Slack** notifiche su fallimento backup,
  per che la notificazione sia automatica su incidente.

### 4. Trasporto NETCONF

- **Piano**: implementare **trasporto NETCONF** per comunicare con devices NETCONF.

### Possibili altre feature (non specificate)

- **Trasporto serial console** (per devices serial).
- **Trasporto REST / API vendor** (per devices API cloud).
- **Backup automatico con plugin LibreNMS** (integrazione più integrata).
- **Backup in tempo reale** (streaming).

---

## Note

Il roadmap è basato sul **README Roadmap** del progetto. Le feature future
verranno implementate successive al livello attuale. La priorità attuale è la
robustezza, l'integrazione LibreNMS, e l'espansione piattaforme supportata.

---

## Riepilogo roadmap

| Priorità | Feature | Descrizione |
|----------|---------|-------------|
| 1 | Pinned host keys | `known_hosts` per sicurezza SSH |
| 2 | Backup concurrente | Worker pool per `--all` |
| 3 | Webhook/Slack notifiche | Notificazione su fallimento backup |
| 4 | Trasporto NETCONF | Comunicazione NETCONF |

---

## Consiglio

- **SSH host key pinning** è importante per sicurezza in ambiti produttivi.
- **Backup concurrente** è utile per schedularesti con molti dispositivi.
- **Notifiche webhook/Slack** migliorano l'incidente response.
- **Trasporto NETCONF** è utile per devices NETCONF che non supportano SSH.
