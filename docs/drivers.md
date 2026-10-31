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
    Strip        []*regexp.Regexp  // whole lines to drop (volatile headers, etc.)
    StripBlocks  []*BlockStrip     // multi-line regions to drop or mask (volatile PEM blocks)
    Mask         []*MaskRule       // volatile values to mask in place, keeping the line
    RawNormalize bool              // true disables generic timestamp/date masking

    // Rarely needed; see the OpenWrt driver.
    Transport    string                          // preferred transport, e.g. "ssh-exec"
    BinarySafe   bool                            // byte-exact capture (Target.RawOutput, no framing newline)
    PostProcess  func(string) (string, error)    // reformat the capture before Clean
    Prepare      func(Driver) Driver             // per-run setup (e.g. a random boundary)
}
```

`Clean(raw)` runs these steps:

1. drop or mask multi-line regions matching a `StripBlocks` rule;
2. drop whole lines matching any `Strip` rule;
3. rewrite values matching a `Mask` rule;
4. mask inline dynamic strings — timestamps, dates, uptimes — via
   `internal/normalize` (skipped if `RawNormalize` is true);
5. trim trailing whitespace, ensure one trailing newline.

## Change stability

The single most important property of a driver is that **two backups of an
unchanged device produce identical bytes**. Two layers cooperate:

- **`Strip`** removes whole volatile lines, e.g. NX-OS `!Time: ...`, IOS
  `Building configuration...`, or the RouterOS `# <date> by RouterOS` header.
- **`StripBlocks`** handles multi-line regions: from a line matching `Start`
  through the first line matching `End`, both inclusive. `End` is first tried
  on the rest of the `Start` line, so a region opened and closed on one line
  ends there. With `Mask` set, the `Start` and `End` lines are kept and only
  the lines between are replaced by the `Mask` line; otherwise the region is
  dropped. If `End` never matches, the lines are **kept**: a missing
  terminator never silently drops the rest of the config.
- **`Mask`** rewrites a volatile value inside a line you want to keep, e.g.
  FortiOS `set password ENC <ciphertext>` becomes `set password ENC
  <ENCRYPTED>`. Prefer it to `Strip` for secrets: the line still shows that a
  secret is set, and adding or removing one still shows in the diff.
- **Generic normalisation** (`internal/normalize`) masks dynamic *substrings*
  embedded in lines you otherwise want to keep — `! Last configuration change at
  10:02:11 UTC Tue Jun 16 2026` becomes `! Last configuration change at
  <TIMESTAMP>`. This is global, so a new platform benefits automatically; only
  add `Strip` rules for volatile content the normaliser does not recognise.

Set `RawNormalize: true` only if masking would corrupt a platform's config.

### Writing strip rules: `backup run --raw`

`rusted backup run --raw NAME` connects to one device and prints its capture
to stdout exactly as received (after any `PostProcess`, with only line endings
and trailing whitespace normalised): no `StripBlocks`, `Strip` or `Mask` rules
and no dynamic-string masking. Nothing is committed and no backup run is
recorded, because the output can hold secrets the normal backup masks. Run it
twice against an unchanged device and diff the two outputs to find what needs
a rule.

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

> Fortinet note: FortiOS bumps `#conf_file_ver=` and re-encrypts every stored
> secret with fresh ciphertext on each save, so an unchanged FortiGate would
> otherwise diff every run. The driver strips `#conf_file_ver=`, masks the value
> of every `set <x> ENC <ciphertext>` line as `<ENCRYPTED>`, and replaces the
> body of `ENCRYPTED ... PRIVATE KEY` PEM blocks with `<ENCRYPTED>` (keeping
> the `set private-key "-----BEGIN ...` and `-----END ...` lines). Plain
> certificates and public keys are kept verbatim. **A Fortinet backup therefore
> holds no restorable secrets**: passwords, PSKs and private keys must be
> re-entered after restoring from it, and a changed secret does not show up as
> a diff (an added or removed one does).

> MikroTik note: RouterOS won't return a full config over the binary API (`/export`
> yields nothing over the API, and `/file` reads are capped at ~4KB). So MikroTik is
> backed up over **SSH** with the `mikrotik_routeros` driver. If SSH auth is a problem,
> `POST /api/provision/mikrotik-ssh-key` installs a generated key over the API and hands
> back the private key to back up with (see internal/provision).

## OpenWrt

OpenWrt has no `show running-config`: its configuration is files. The
`openwrt` driver captures every `/etc/config/*` file plus `/etc/sysupgrade.conf`
and everything it lists (plain files, shell globs such as `/root/scripts/*`, and
directories, walked recursively with hidden files included), i.e. the set
sysupgrade itself preserves. Entries that do not exist are skipped.

> **Secrets end up in git.** Unlike the network-OS drivers nothing is masked:
> anything `/etc/sysupgrade.conf` lists is committed verbatim, which commonly
> includes `/etc/shadow`, `/etc/dropbear/` host and authorized keys, VPN keys
> and certificates. Wi-Fi passphrases (`option key` in `/etc/config/wireless`)
> and other credentials in UCI files are committed too. Protect the backups
> repository accordingly, and trim `sysupgrade.conf` if that is not acceptable.

How it works:

- **Read-only, minimal device side.** Only shell builtins (`echo`, `read`,
  `test`, `for`, `case`, `set`) plus `cat` and `ls` run on the device; no `od`,
  `find`, `head`, `base64` or other applets that a busybox build may lack, and
  nothing is written.
- **Byte-exact transport.** The driver pins `ssh-exec` and sets `BinarySafe`, so
  the exec channel returns stdout untouched (no PTY, no CRLF folding). Don't pin
  an openwrt device to the interactive `ssh` transport.
- **Unambiguous framing.** Each file is preceded by a marker line carrying a
  random boundary generated by rusted for that run (`Prepare`), MIME-style, so
  no file content can fake or break the framing. Each command ends with an
  `END` marker; a capture missing one fails instead of committing a partial
  config. The boundary never reaches the stored file.
- **Limits.** Symlinks are recorded as `(symlink, not followed)` and never
  followed, so a link cannot loop the walk or pull in a large target (as in
  sysupgrade's own archive). Files over 1 MiB are recorded as `(skipped: ...)`.
- **Stored format.** `PostProcess` writes one `#### file: <path>` section per
  file. A file holding a NUL byte or invalid UTF-8 is stored as base64 of its
  exact bytes, wrapped at 76 columns, under `#### file: <path> (binary, N bytes,
  base64)`, and decodes back byte-for-byte. Text files are stored as text, so
  like every other driver their trailing whitespace is trimmed and CRLF folded;
  `RawNormalize` is set so dates inside scripts are never rewritten.

The driver has no `Strip` or `Mask` rules: nothing on a stock OpenWrt is
volatile enough to need one. Add rules when real-world noise shows up.

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
