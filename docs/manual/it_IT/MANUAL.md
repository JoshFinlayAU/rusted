# rusted — Manuale Completo

**rusted: Tool di backup delle configurazioni degli dispositivi di rete**

A network device configuration backup tool — una modern, single-binary
sostituto per **RANCID** e **Oxidized**.

---

## Panorama

rusted è uno strumento Go che collega i dispositivi di rete (Cisco, Juniper,
MikroTik, e altri) attraverso SSH, raccoglie la configurazione attiva (running
config) usando driver specifici per ogni piattaforma, maschera variabili volatile
per rendere i backup stabili alla differenza, e versiona ogni modifica in un
repository git. Le credenziali e l'inventario di dispositivi vivono in una base
SQLite locale, e un'API HTTP avvolge l'integrazione con **LibreNMS**.

### Caratteristiche principali

- **Trasporti pluggabili** (SSH built-in; telnet, ssh-exec e altri aggiungibili)
- **Supporto Cisco enable-mode** — automatico escalazione di privilegio
- **Timeout per dispositivo** — `--cmd-timeout`, `--idle-timeout` personalizzati
- **Driver per piattaforma** — disabilita paging, disegna config, maschera variabili
- **Backup stabili alla differenza** — variabili volatili stripite e maskate
- **Storage versioneggiato in git** — un file config per dispositivo, in `./backups`
- **Basse SQLite** — credenziali e dispositivi, con encryption-at-rest opzionale
- **API HTTP** + **integrazione LibreNMS** — aggiunta/removal dispositivi, history, trigger backup
- **Nessun Python**, nessuna dipendenza runtime oltre `git` — binario Go singolo

### Requisiti preliminaari

- **Go 1.26+** (toolchain per build e runtime)
- **git** (per storage dei backup)
- Per la LibreNMS: LibreNMS con il sistema di plugin v2 (`app/Plugins`)

---

## Tabella di riepilogo delle sezioni

| N. | Titolo | Descrizione |
|----|--------|-------------|
| 1 | **Indice e panorama** | Questo file: introduzione, panorama, requisiti preliminaari |
| 2 | **Installazione** | `install.sh`, installazione manuale, post-install |
| 3 | **Configurazione** | File `config.toml`, variabili d'ambiente, precedenza, init/show |
| 4 | **Riassunto comandi CLI** | Tabella completa di tutti i comandi con scopo e flag |
| 5 | **Comandi init/config/driver** | `rusted init`, `rusted config`, `rusted driver list` |
| 6 | **Comandi cred/device** | `rusted cred`, `rusted device` (add/list/update/rename/remove/enable/disable) |
| 7 | **Comandi backup/serve** | `rusted backup run`, `rusted backup history`, `rusted serve` |
| 8 | **Encryption-at-rest** | AES-256-GCM, testo/encriptato, gestione del secret, sicurezza |
| 9 | **Backup e differenza di cambiamenti** | Meccanismo change detection, Strip, normalize, `--raw` |
| 10 | **Storage in repository git** | Repo `./backups`, rename, remove, purge-history, revisioni |
| 11 | **API HTTP** | Tutti gli endpoint `/api/*`, autenticazione, responses, errori |
| 12 | **Integrazione LibreNMS** | Installazione plugin, UI, endpoints, troubleshooting |
| 13 | **Schedulazione backup** | Cron, systemd timer, service, flock |
| 14 | **Piattaforme supportate** | Tutti i driver, notes specifiche, DRAFT |
| 15 | **Meccanismi trasporto** | SSH, telnet, ssh-exec, interface pluggabile, guide modulo |
| 16 | **Reporting errori** | Formato messaggi, timeout, paginazione, dump troncati |
| 17 | **Roadmap futuro** | Plan di sviluppo attuali |
| 18 | **Troubleshooting** | Problemi comuni e soluzioni |
| 19 | **Glosario** | Termini tecnici usati nel manuale |

---

## Come usare il manuale

Questo manuale è strutturato per essere consultato in modo lineare:
1. **Panorama e requisiti** (Sezione 1) per capire cosa serve.
2. **Installazione e configurazione** (Sezioni 2–3) per preparare il programma.
3. **Comandi CLI** (Sezioni 4–7) per usare l'utente.
4. **Funzionamento interno** (Sezioni 8–11) per capire il meccanismo.
5. **Funzionalità avanzate** (Sezioni 12–15) per integrazioni e specifiche.
6. **Supporto operativo** (Sezioni 16–19) per troubleshooting e riferimenti.

---

## File del manuale

Tutti i file del manuale sono presenti nella directory `docs/manual/`:

```
docs/manual/
├── MANUAL.md                          # Indice e panorama (questo file)
├── MANUAL-01-installation.md          # Installazione
├── MANUAL-02-configuration.md         # Configurazione
├── MANUAL-03-cli-overview.md          # Riassunto comandi CLI
├── MANUAL-04-cli-init-config-driver.md # Comandi init/config/driver
├── MANUAL-05-cli-cred-device.md       # Comandi cred/device
├── MANUAL-06-cli-backup-serve.md      # Comandi backup/serve
├── MANUAL-07-credential-encryption.md # Encryption-at-rest
├── MANUAL-08-backup-change-detection.md # Backup e differenza di cambiamenti
├── MANUAL-09-git-storage.md           # Storage in repository git
├── MANUAL-10-http-api.md              # API HTTP
├── MANUAL-11-librenms-integration.md  # Integrazione LibreNMS
├── MANUAL-12-scheduled-backups.md     # Schedulazione backup
├── MANUAL-13-supported-platforms.md   # Piattaforme supportate
├── MANUAL-14-transport-mechanisms.md  # Meccanismi trasporto
├── MANUAL-15-error-reporting.md       # Reporting errori
├── MANUAL-16-roadmap.md               # Roadmap futuro
├── MANUAL-17-troubleshooting.md       # Troubleshooting
└── MANUAL-18-glossary.md              # Glosario
```
