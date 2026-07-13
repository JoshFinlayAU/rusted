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
  `Building configuration...`, or the RouterOS `# <date> by RouterOS` header.
- **Generic normalisation** (`internal/normalize`) masks dynamic *substrings*
  embedded in lines you otherwise want to keep — `! Last configuration change at
  10:02:11 UTC Tue Jun 16 2026` becomes `! Last configuration change at
  <TIMESTAMP>`. This is global, so a new platform benefits automatically; only
  add `Strip` rules for volatile content the normaliser does not recognise.

Set `RawNormalize: true` only if masking would corrupt a platform's config.

## Built-in drivers

`rusted driver list` prints them. The platforms named as supported in the
project spec are:

| Driver              | Platform               | Config command            |
|---------------------|------------------------|---------------------------|
| `cisco_nxos`        | Cisco Nexus (NX-OS)    | `show running-config`     |
| `mikrotik_routeros` | MikroTik RouterOS v7+  | `/export terse`           |
| `mikrotik_routeros_api` | MikroTik RouterOS v7+ over the binary API | `/export` |
| `juniper_junos`     | Juniper Junos          | `show configuration | display set` |

Additional drivers (`cisco_ios`, `cisco_asa`, `arista_eos`, `fortinet`,
`vyos`, `generic`) ship as well.

`mikrotik_routeros_api` is for devices with SSH disabled but the API enabled: pair it
with the `routeros-api` transport (set the device's `transport` to `routeros-api`, port
8728, or 8729 for API-SSL). It runs `/export` as an API sentence - a RouterOS 7 behaviour,
so validate it against your gear.

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
