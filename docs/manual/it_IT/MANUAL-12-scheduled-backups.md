# Sezione 12 — Schedulazione dei backup

## Obiettivo

Spiega come **schedulare** i backup in rusted tramite cron e sistema d'admin (systemd timer).

---

## Risorse

`rusted backup run --all` backup di **tutti i dispositivi attivi** e poi esita —
idea ideale per un scheduler. Esita **non-0** se ci sono errori, per che
cron/systemd percepisano i fallimenti.

> **Importante**: questa risorsa è **indipendente** da `rusted serve`. Puoi avviare
> l'API (`--service` nell'installer) e un timer backup in parallelo.

---

## Option A — Cron

Modifica il crontab dell'utente che possiede la directory dati (`crontab -e`, o
`sudo crontab -e` per installazione globale posseduta da root):

```cron
# Backup tutti i dispositivi ogni giorno alle 02:00, una volta per volta, con log.
0 2 * * * /usr/bin/flock -n /tmp/rusted-backup.lock \
  /usr/local/bin/rusted --config /etc/rusted/config.toml backup run --all \
  >> /var/log/rusted-backup.log 2>&1
```

### Caratteristiche

- **`flock -n`**: blocca un run iniziato lentamente dall'altro, per non sovrapporre
  run.
- **Mailto**: aggiungi `MAILTO=you@example.com` all'inizio del crontab per essere
  mandata mail su fallimento (cron manda qualsiasi output; l'esitazione non-0
  juga flag.
- **Installazione utente**: usa i pathi del tuo utente, es.
  `~/.local/bin/rusted --config ~/.config/rusted/config.toml backup run --all`.
- **Passare sempre `--config`** (ambiente scheduler è minimale) e un path absolu
  al binario.

### Esempio cron completo (globale, utente root)

```cron
# Mailto su fallimento.
MAILTO=backup@example.com

# Backup tutti i dispositivi ogni giorno alle 02:00 con flock e log.
0 2 * * * /usr/bin/flock -n /tmp/rusted-backup.lock \
  /usr/local/bin/rusted --config /etc/rusted/config.toml backup run --all \
  >> /var/log/rusted-backup.log 2>&1
```

### Esempio cron (utente)

```cron
# Backup tutti i dispositivi ogni giornata alle 02:00.
0 2 * * * /home/utente/.local/bin/rusted --config /home/utente/.config/rusted/config.toml backup run --all >> /home/utente/log/rusted-backup.log 2>&1
```

---

## Option B — systemd timer

Meno robusto di cron (logging via journald, no overlap, status facile). Crea due unit files:

### `/etc/systemd/system/rusted-backup.service`

```ini
[Unit]
Description=rusted — back up all network device configs
After=network-online.target
Wants=network-online.target

[Service]
Type=oneshot
ExecStart=/usr/local/bin/rusted --config /etc/rusted/config.toml backup run --all
# User=rusted        # se rusted si esegue sotto un account dedicato
```

### `/etc/systemd/system/rusted-backup.timer`

```ini
[Unit]
Description=Run rusted backups on a schedule

[Timer]
OnCalendar=*-*-* 02:00:00
Persistent=true          # cattura la corsa se la machine è stata off a 02:00
RandomizedDelaySec=300   # opzionale: jitter per evitare carichi tutti in una volta

[Install]
WantedBy=timers.target
```

### Abilita e verifica

```sh
sudo systemctl daemon-reload
sudo systemctl enable --now rusted-backup.timer
systemctl list-timers rusted-backup.timer   # conferma l'run successivo
journalctl -u rusted-backup.service         # visualizza output backup
```

### Esecuzione manuale una volta

```sh
sudo systemctl start rusted-backup.service
```

---

## Best practices per schedulazione

1. **Usa `flock`** (cron) o **`Persistent=true`** (systemd) per evitare overlap.
2. **Passare sempre `--config`** a rusted nel scheduler.
3. **Abilita `network-online.target`** (service) per che backup si eseguono dopo l'online.
4. **Mailto su fallimento** per che la notificazione sia ricevuta (cron) o
   journald (systemd).
5. **Jitter (`RandomizedDelaySec`)** per systemd per evitare carichi simultanei sui
   dispositivi.
6. **Testa il timer** con `systemctl list-timers` e `journalctl` prima di abilitare.

---

## Riepilogo

| Modo | Tool | Vantaggi |
|------|------|----------|
| Cron | `flock` + crontab | Simple, standard |
| systemd timer | `timer` + `service` unit | Robust, journald, no overlap |
| Service singolo | `rusted serve` + systemd | API sempre attivo, backup da timer |

> Consiglio: usa **systemd timer** per la robustness, oppure **cron con `flock`**
> per la simplicità.
