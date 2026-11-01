package main

import (
	"runtime/debug"
	"strings"
)

// version is stamped by release builds: -ldflags "-X main.version=1.2.3".
var version = ""

// buildVersion is the stamped version, else what the Go toolchain recorded
// (a module version for `go install`, a commit for a source build), else "dev".
func buildVersion() string {
	if version != "" {
		return version
	}
	bi, ok := debug.ReadBuildInfo()
	if !ok {
		return "dev"
	}
	if v := bi.Main.Version; v != "" && v != "(devel)" {
		return strings.TrimPrefix(v, "v")
	}
	for _, s := range bi.Settings {
		if s.Key == "vcs.revision" && len(s.Value) >= 12 {
			return "dev-" + s.Value[:12]
		}
	}
	return "dev"
}
