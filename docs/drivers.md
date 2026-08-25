# Writing a platform driver

A **driver** holds per-platform knowledge: which commands disable paging, which
commands dump the configuration, and how to make that output *change-stable* so
an unchanged device produces an unchanged file (and therefore no git commit).

Drivers are data — see `internal/driver/driver.go`. Adding a platform usually
means appending one `Driver` literal to `builtins()`.

## The struct

```go
type Driver struct {
    Name         string            // selector, e.g. "cisco_nxos"
    Description  string
    Init         []string          // run once after login (disable paging, enter enable, ...)
    Config       []string          // output of these = the saved configuration
    Strip        []*regexp.Regexp  // whole lines to drop (volatile headers, rotating secrets, ...; skipped by --raw)
    StripBlocks  []*BlockStrip     // multi-line regions to drop (volatile PEM blocks; Start..End inclusive, skipped by --raw)
    RawNormalize bool              // true disables generic timestamp/date masking
}
```

`Clean(raw)` runs three steps:

1. drop whole lines matching any `Strip` rule;
2. mask inline dynamic strings — timestamps, dates, uptimes — via
   `internal/normalize` (skipped if `RawNormalize` is true);
3. trim trailing whitespace, ensure one trailing newline.

## Change stability

The single most important property of a driver is that **two backups of an
unchanged device produce identical bytes**. Two layers cooperate:

- **`Strip`** removes whole volatile lines, e.g. NX-OS `!Time: ...`, IOS
  `Building configuration...`, the RouterOS `# <date> by RouterOS` header, or
  FortiOS's rotating `#conf_file_ver=` header and re-encrypted secrets
  (`set password ENC …`, fresh ciphertext on every save).
- **`StripBlocks`** removes multi-line regions — from a line matching `Start`
  through the first following line matching `End`, both inclusive. Use for
  volatile PEM blocks: FortiGate re-encrypts stored private keys with a new
  salt/IV on every save, so the whole `set private-key "-----BEGIN ENCRYPTED
  PRIVATE KEY-----…` block changes between identical dumps. Only *encrypted*
  blocks are volatile — plain certificates and public keys are deterministic
  and must be kept.
- **Generic normalisation** (`internal/normalize`) masks dynamic *substrings*
  embedded in lines you otherwise want to keep — `! Last configuration change at
  10:02:11 UTC Tue Jun 16 2026` becomes `! Last configuration change at
  <TIMESTAMP>`. This is global, so a new platform benefits automatically; only
  add `Strip` rules for volatile content the normaliser does not recognise.

All `Strip` rules are applied by default on every backup. `rusted backup run
--raw` bypasses them entirely (and skips normalisation too), storing the
verbatim capture — useful when you need to audit exactly what the device
emitted, at the cost of a commit per run.

Set `RawNormalize: true` only if masking would corrupt a platform's config.

## Built-in drivers

`rusted driver list` prints them. The platforms named as supported in the
project spec are:

| Driver              | Platform               | Config command            |
|---------------------|------------------------|---------------------------|
| `cisco_nxos`        | Cisco Nexus (NX-OS)    | `show running-config`     |
| `mikrotik_routeros` | MikroTik RouterOS v7+  | `/export terse`           |
| `juniper_junos`     | Juniper Junos          | `show configuration | display set` |

Additional drivers (`cisco_ios`, `cisco_asa`, `arista_eos`, `fortinet`,
`openwrt`, `vyos`, `generic`) ship as well.

> MikroTik note: RouterOS won't return a full config over the binary API (`/export`
> yields nothing over the API, and `/file` reads are capped at ~4KB). So MikroTik is
> backed up over **SSH** with the `mikrotik_routeros` driver. If SSH auth is a problem,
> `POST /api/provision/mikrotik-ssh-key` installs a generated key over the API and hands
> back the private key to back up with (see internal/provision).

## OpenWrt

OpenWrt is a Linux distribution: there is no `show running-config`, the
configuration *is* the filesystem. The driver captures over **ssh-exec**
(byte-exact, no PTY) and keeps the device side deliberately minimal:

- **Only ash builtins (`for/case/read/test/echo`) plus `cat` are used** — no
  `od`, `find`, `sort`, `sed`, `head` or `base64`, which are not guaranteed to
  exist in every busybox build. All formatting happens on the rusted side
  (`PostProcess`), including binary detection and base64 encoding via the Go
  standard library.
- Two commands run on the device, each file introduced by a `## <path>` marker:
  1. every `/etc/config/*` file (the UCI configuration);
  2. `/etc/sysupgrade.conf` itself, then each non-comment entry expanded:
     plain files (`/etc/init.d/connection_monitor`), shell globs
     (`/root/scripts/*`) and directories (`/etc/openvpn/`, trailing slash
     optional) resolved recursively by a small shell function. Entries that do
     not exist are skipped silently.
- Sections whose first 4 KiB contain a NUL byte are considered **binary** and
  stored as base64 of the exact received bytes (marker
  `## binary file, base64 follows`), keeping the whole backup text-only,
  byte-faithful and diff-stable.

Because capture is byte-exact, ordering is stable across runs (shell glob
order is alphabetical per directory; entries are processed in manifest order).

The driver's `Strip`/`StripBlocks` lists are intentionally empty — nothing on a
stock OpenWrt is volatile enough to strip. Add rules only when real-world noise
shows up.

## Cambium (drafts)

Two Cambium platforms expose their config over an SSH command and get **draft** drivers
(marked DRAFT in the description - validate against real gear before you trust them):

| Name | Platform | Config command |
|---|---|---|
| `cambium_epmp` | Cambium ePMP (AP/SM) | `config show json` |
| `cambium_cnmatrix` | Cambium cnMatrix switch | `show running-config` |

- **ePMP** dumps its whole config as JSON. The `cfgUtcTimestamp` line is stripped so an
  unchanged unit doesn't diff every run; confirm the JSON is one-field-per-line on your
  firmware (if it's minified onto a single line, that strip would drop everything - adjust).
- **cnMatrix** is Cisco-like; the paging-disable `Init` command is a best guess - if the
  switch pages output, change it to whatever your firmware uses.

The rest of the Cambium/PTP range does **not** expose config over an SSH stdout command, so
they can't be plain drivers:

- **PMP450 / PTP650** - config only comes from the **web GUI** (oxidized scrapes the HTTP
  `.cfg` file) or SNMP. Needs an HTTP transport (not built yet).
- **PTP820 / PTP850** (Ceragon IP-20 based) - config is exported as a **file pushed to an
  FTP/SFTP server**, not printed to a CLI session. Needs a file-fetch step, similar to the
  MikroTik `/export file=` case.

Those three are noted here so nobody assumes an SSH driver will cover them.

## Example

```go
{
    Name:        "cisco_nxos",
    Description: "Cisco NX-OS",
    Init:        []string{"terminal length 0"},
    Config:      []string{"show running-config"},
    Strip: []*regexp.Regexp{
        re(`^!Time:`),
        re(`^!Running configuration last done at`),
    },
},
```
