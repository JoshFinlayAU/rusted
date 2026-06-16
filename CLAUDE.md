# rusted
## a network device configuration backup tool. It's a RANCID + Oxidized replacement!

## Transport Mechanisms

Initially just SSH, but make it modular and document how to build a transport module.

## Credentials 

Credentials will be stored in an SQLite database.

CLI tools will be written to read/write/manage the credentials and routers.

## Integration

Offer full integration with LibreNMS.

Develop a LibreNMS module to support:

- Add/removal of device(s)
- View backup history
- Trigger backup, visual feedback etc

## Backup Storage

Backups will be stored in a git repository located in ./backups

## RULES

Work autonomously.
Do NOT use Python.

## Supported Network Devices

- Cisco Nexus (NX-OS)
- Mikrotik RouterOS version 7+
- Juniper (JunOS)
