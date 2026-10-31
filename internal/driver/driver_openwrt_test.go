package driver

import (
	"encoding/base64"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func TestOpenWrtDriverContract(t *testing.T) {
	d, ok := Get("openwrt")
	if !ok {
		t.Fatal("openwrt driver not registered")
	}
	if d.Transport != "ssh-exec" || !d.BinarySafe || d.Prepare == nil {
		t.Fatalf("openwrt needs ssh-exec, BinarySafe and Prepare: %+v", d)
	}
	a, b := d.Prepare(d), d.Prepare(d)
	if a.Prepare != nil || a.PostProcess == nil || len(a.Config) != 2 {
		t.Fatalf("prepared driver incomplete: %+v", a)
	}
	if a.Config[0] == b.Config[0] {
		t.Fatal("each run must get a fresh boundary")
	}
}

// The device-side commands must stay read-only: only shell builtins, cat and
// ls, and no output redirection except discarding stderr.
func TestOpenWrtCommandsReadOnly(t *testing.T) {
	allowed := map[string]bool{
		"b": true, "f": true, "if": true, "then": true, "elif": true, "else": true, "fi": true,
		"for": true, "do": true, "done": true, "while": true, "read": true, "case": true,
		"esac": true, "continue": true, "echo": true, "set": true, "cat": true, "ls": true,
		"[": true, "e": true, "p": true, "in": true,
	}
	word := regexp.MustCompile(`(?:^|[;{}|&]\s*|\$\(|\bthen\s+|\belse\s+|\bdo\s+)([A-Za-z\[][A-Za-z0-9_]*)`)
	for _, cmd := range openwrtCommands("rusted-TEST") {
		for _, m := range word.FindAllStringSubmatch(cmd, -1) {
			if !allowed[m[1]] {
				t.Errorf("unexpected command %q in %s", m[1], cmd)
			}
		}
		if s := strings.ReplaceAll(cmd, "2>/dev/null", ""); strings.Contains(s, ">") {
			t.Errorf("command writes somewhere: %s", cmd)
		}
	}
}

func decodeSection(t *testing.T, out, path string) []byte {
	t.Helper()
	hdr := "#### file: " + path + " (binary, "
	i := strings.Index(out, hdr)
	if i < 0 {
		t.Fatalf("no binary section for %s in:\n%s", path, out)
	}
	rest := out[i:]
	rest = rest[strings.Index(rest, "\n")+1:]
	if j := strings.Index(rest, "\n#### file: "); j >= 0 {
		rest = rest[:j]
	}
	dec, err := base64.StdEncoding.DecodeString(strings.Join(strings.Fields(rest), ""))
	if err != nil {
		t.Fatalf("%s: bad base64: %v", path, err)
	}
	return dec
}

func TestOpenWrtPostProcess(t *testing.T) {
	const bd = "rusted-TESTBOUNDARY"
	bin := "\x00\x01\xff" + strings.Repeat("AB\x00CD\r\n", 50) + "\n## /etc/fake\nno newline"
	text := "config system\n\toption hostname 'r1'\n## not a marker\n#### file: nor this\n"
	raw := "\n" + bd + " F /etc/config/system\n" + text +
		"\n" + bd + " F /root/tool\n" + bin +
		"\n" + bd + " F /etc/nolf\nlast line" +
		"\n" + bd + " F /etc/empty\n" +
		"\n" + bd + " S /root/big.img\n" +
		"\n" + bd + " L /etc/loop\n" +
		"\n" + bd + " END\n" +
		"\n" + bd + " F /etc/config/system\nduplicate\n" +
		"\n" + bd + " END\n"
	out, err := openwrtPostProcess(raw, bd, 2)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, bd) {
		t.Fatal("boundary leaked into the stored config")
	}
	if !strings.Contains(out, "#### file: /etc/config/system\n"+text) {
		t.Errorf("text section not verbatim:\n%s", out)
	}
	if strings.Contains(out, "duplicate") {
		t.Error("a repeated path must keep its first capture only")
	}
	if got := decodeSection(t, out, "/root/tool"); string(got) != bin {
		t.Errorf("binary round-trip: got %d bytes, want %d", len(got), len(bin))
	}
	for _, s := range []string{
		"#### file: /etc/nolf\nlast line\n",
		"#### file: /etc/empty\n",
		"#### file: /root/big.img (skipped: over 1048576 bytes)\n",
		"#### file: /etc/loop (symlink, not followed)\n",
	} {
		if !strings.Contains(out, s) {
			t.Errorf("missing %q in:\n%s", s, out)
		}
	}

	// Binary bytes must also survive Clean, which is what gets committed.
	d, _ := Get("openwrt")
	if got := decodeSection(t, d.Clean(out), "/root/tool"); string(got) != bin {
		t.Error("binary changed by Clean")
	}

	if _, err := openwrtPostProcess(strings.TrimSuffix(raw, "\n"+bd+" END\n"), bd, 2); err == nil {
		t.Error("a capture missing an END marker must fail")
	}
}

// Run the real device-side commands with a local sh against a fake root, then
// post-process: exercises globbing, recursion, symlink loops, the size cap and
// byte-exact binaries end to end.
func TestOpenWrtCommandsEndToEnd(t *testing.T) {
	sh, err := exec.LookPath("sh")
	if err != nil {
		t.Skip("no sh")
	}
	root := t.TempDir()
	write := func(p, s string) {
		full := filepath.Join(root, p)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(s), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	bin := "\x7fELF\x00\x00binary-without-newline"
	write("etc/config/network", "config interface 'lan'\n\toption ipaddr '192.168.1.1'\n")
	write("etc/config/system", "config system\n")
	write("etc/dropbear/authorized_keys", "ssh-ed25519 AAAA test\n")
	write("etc/openvpn/client.conf", "remote vpn.invalid\n")
	write("etc/openvpn/.hidden", "dotfile\n")
	write("root/scripts/tool", bin)
	write("root/big.img", strings.Repeat("x", openwrtMaxFile+1))
	if err := os.Symlink(filepath.Join(root, "etc"), filepath.Join(root, "etc/openvpn/loop")); err != nil {
		t.Fatal(err)
	}
	// Comment, blank line, trailing-slash dir, glob, missing entry, and a last
	// line without a newline.
	write("etc/sysupgrade.conf", "# keep these\n\n"+
		root+"/etc/dropbear/authorized_keys\n"+
		root+"/etc/openvpn/\n"+
		root+"/root/scripts/*\n"+
		root+"/does/not/exist\n"+
		root+"/root/big.img")

	const bd = "rusted-TESTBOUNDARY"
	var raw strings.Builder
	cmds := openwrtCommands(bd)
	for _, c := range cmds {
		c = strings.ReplaceAll(c, " /etc/", " "+root+"/etc/")
		out, err := exec.Command(sh, "-c", c).Output()
		if err != nil {
			t.Fatalf("%v running %s", err, c)
		}
		raw.Write(out)
	}
	out, err := openwrtPostProcess(raw.String(), bd, len(cmds))
	if err != nil {
		t.Fatalf("%v\nraw:\n%q", err, raw.String())
	}
	for _, s := range []string{
		"#### file: " + root + "/etc/config/network\nconfig interface 'lan'\n",
		"#### file: " + root + "/etc/config/system\nconfig system\n",
		"#### file: " + root + "/etc/sysupgrade.conf\n# keep these\n",
		"#### file: " + root + "/etc/dropbear/authorized_keys\nssh-ed25519 AAAA test\n",
		"#### file: " + root + "/etc/openvpn/client.conf\nremote vpn.invalid\n",
		"#### file: " + root + "/etc/openvpn/.hidden\ndotfile\n",
		"#### file: " + root + "/etc/openvpn/loop (symlink, not followed)\n",
		"#### file: " + root + "/root/big.img (skipped: over 1048576 bytes)\n",
	} {
		if !strings.Contains(out, s) {
			t.Errorf("missing %q in:\n%s", s, out)
		}
	}
	if got := decodeSection(t, out, root+"/root/scripts/tool"); string(got) != bin {
		t.Errorf("binary round-trip: got %q, want %q", got, bin)
	}
	if strings.Contains(out, "#### file: "+root+"/does/not/exist") {
		t.Error("missing entries must be skipped silently")
	}
}
