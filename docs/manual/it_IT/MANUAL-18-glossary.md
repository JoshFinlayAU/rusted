# Sezione 18 — Glosario

## Obiettivo

Glosario dei **termini tecnici** usati nel manuale e nel programma rusted.

---

## A

| Termine | Descrizione |
|---------|-------------|
| **API HTTP** | Interfaccia HTTP di rusted (`rusted serve`), endpoint `/api/*` per gestione dispositivi/credenziali/backup. |
| **API token** | Token Bearer usato per autenticare gli endpoint API rusted. Provienza dal config file o `RUSTED_API_TOKEN`. |
| **Auth** | Autenticazione (login), usata per connettersi ai dispositivi via credenza (password/key). |
| **Backup** | Raccolta della configurazione attiva di un dispositivo e versionamento in repository git. |

## B

| Termine | Descrizione |
|---------|-------------|
| **Binario** | Il programma rusted, binario Go singolo. |
| **Builtin** | Driver o trasporto predefinito di rusted (es. SSH, driver cisco_nxos). |

## C

| Termine | Descrizione |
|---------|-------------|
| **Change-stable** | Stabile alla differenza: due backup identici producono byte identici (no commit spuriousi). |
| **Credential** | Credenza di login per un dispositivo: username, password, optional key/PEM e password enable. |
| **Commit** | Hash di un commit git del repository backup. |
| **Config** | Configurazione attiva del device (running config). |
| **Configurazione** | File/ambiente che configura rusted. |

## D

| Termine | Descrizione |
|---------|-------------|
| **Debug** | Output dettagliato di I/O device (comandi inviati e byte ricevuti), `--debug`/`-vv`. |
| **Driver** | Driver piattaforma di rusted: knows come disabilita paging, disegna config, strip volatili. |
| **Disable** | Mark dispositivo come inattivato (stanca backup ma conserva storia). |
| **Diff** | Differenza unificata tra versioni di configurazione. |
| **Diff-stable** | Stabile alla differenza (same as change-stable). |
| **DRAFT** | Driver marcato come in development, deve essere validato contro la realtà prima. |

## E

| Termine | Descrizione |
|---------|-------------|
| **Enable-mode** | Mode privilegiato di Cisco IOS/ASA (`#` prompt), richiesto per comandi come `show running-config`. |
| **Encryption-at-rest** | Crittografia delle credenziali nel SQLite, tramite AES-256-GCM. |
| **End-to-end** | Intero flusso: da CLI all'API, dal device al repository. |
| **Error budget** | (LibreNMS) Budget di errori nel sistema. |

## F

| Termine | Descrizione |
|---------|-------------|
| **Flock** | Comando di sistema per bloccare file, usato in cron per evitare overlap backup. |
| **FortiGate** | FortiOS del Fortinet, piattaforma supportata con driver `fortinet`. |

## G

| Termine | Descrizione |
|---------|-------------|
| **Generic** | Driver fallback per piattaforme non riconosciute (`generic`). |
| **Git** | Versionamento di git, usato per storage dei backup. |
| **Glob** | Percorso glob che rilegge file in directory (usato in driver openwrt). |

## H

| Termine | Descrizione |
|---------|-------------|
| **Health check** | `/healthz`, controllo di salute server. |

## I

| Termine | Descrizione |
|---------|-------------|
| **Idle timeout** | Window idlesimo (millisecondi) per considerare output del device completo. |
| **Init command** | Comando inviato una volta dopo il login (es. disabilita paging). |
| **Integrazione** | Collegamento tra rusted e LibreNMS. |
| **Irreversibile** | Operazione che non può essere inverta (es. purge-history). |

## K

| Termine | Descrizione |
|---------|-------------|
| **KeyboardInteractive** | Auth SSH interattivo, usato per password interattiva. |
| **Keystroke** | Keystroke inviato al device via trasporto. |

## L

| Termine | Descrizione |
|---------|-------------|
| **Line** | Riga di testo. |
| **LibreNMS** | Sistema di monitoraggio rete di automazione, integrazione con rusted. |
| **List** | Comando che elenca elementi (driver, credenza, dispositivo). |

## M

| Termine | Descrizione |
|---------|-------------|
| **Masking** | Maschera di variabili dinamiche (es. timestamp) con placeholder. |
| **MikroTik** | Piattaforma RouterOS, supportata con driver `mikrotik_routeros`. |
| **More** | Prompt pagina (`--More--`) emesso dal device quando output supera una schermo. |
| **Mode privilegiato** | Sottocategoria del mode enable. |

## N

| Termine | Descrizione |
|---------|-------------|
| **Network** | Sistema di rete. |
| **Normalize** | Normalizzazione di stringhe (linee terminate, spazi trimmi, escapes). |
| **Paging** | Gestione del prompt `--More--` (avanzare pagina). |

## P

| Termine | Descrizione |
|---------|-------------|
| **Password** | Password di login del device. |
| **PEM** | Prefixe per key private/certificate OpenSSH/PKCSX. |
| **Persistent** | `Persistent=true` in timer systemd: cattura corsa se machine off. |
| **Piattaforma** | Piattaforma di rete supportata da un driver. |

## R

| Termine | Descrizione |
|---------|-------------|
| **Raw** | Salva configurazione verbatim, senza stripping/maschera, `--raw`. |
| **RawOutput** | Trasporto che non normalizza output (byte-exact), per binari. |
| **Riepilogo** | Elenco dei risultati. |
| **RouterOS** | Sistema operativo di MikroTik. |

## S

| Termine | Descrizione |
|---------|-------------|
| **Secret** | Segreto di encryption-at-rest (chiave AES-256-GCM) o token API. |
| **Serve** | Avvia l'API HTTP (`rusted serve`). |
| **Session** | Connessione attiva con device tramite trasporto. |
| **Sicurezza** | Aspetto di protezione da vulnerabilità. |
| **Sicurezza at-rest** | (same as encryption-at-rest). |

## T

| Termine | Descrizione |
|---------|-------------|
| **Target** | Carica info di connessione e autenticazione per trasporto. |
| **Text/plain** | Formato di testo raw (config, diff). |
| **Transport** | Meccanismo che connette rusted a device (SSH, telnet, ssh-exec, ...). |

## U

| Termine | Descrizione |
|---------|-------------|
| **Uptime** | Tempo di funzione del device, dinamico. |
| **Upsert** | Aggiorna in-place se esiste, o aggiungi se no. |

## V

| Termine | Descrizione |
|---------|-------------|
| **Verbose** | Output progressivo, `-v`/`--verbose`. |
| **Viatta** | (same as via). |

## W

| Termine | Descrizione |
|---------|-------------|
| **Webhook** | Endpoint esterno per notifica. |
| **Web UI** | Interfaccia web di gestione (LibreNMS). |

## Y

| Termine | Descrizione |
|---------|-------------|
| **Yes/No** | Voglia/non voglia, usato per state boolean (es. ENABLED). |

## Z

| Termine | Descrizione |
|---------|-------------|
| **Z` | Hash commit abbreviato. |
| **Zip** | Archivio compressed (download revisioni). |
