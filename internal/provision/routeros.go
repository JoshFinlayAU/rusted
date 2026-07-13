// Package provision bootstraps key-based SSH access on a device using an out-of-band
// management API, so future backups can run over SSH with a key instead of a password.
//
// MikroTik RouterOS is the first: RouterOS won't hand a full config back over the binary
// API (no /export output, and /file reads are capped at ~4KB), but it will happily install
// an SSH public key over the API. So we generate a keypair, push the public half via the
// API, hand the private half back to the caller (My Mate stores it), and from then on the
// device is backed up over plain SSH with the key.
package provision

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/tls"
	"encoding/pem"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/go-routeros/routeros/v3"
	"golang.org/x/crypto/ssh"
)

// MikrotikResult is what a successful bootstrap hands back to the caller.
type MikrotikResult struct {
	User         string `json:"user"`          // the RouterOS user the key was installed for
	PrivateKey   string `json:"private_key"`   // OpenSSH PEM - store this, it authenticates SSH
	SSHPort      int    `json:"ssh_port"`      // the device's SSH service port
	SSHEnabled   bool   `json:"ssh_enabled"`   // true once we're done (we enable it if it was off)
	SSHEnabledBy bool   `json:"ssh_enabled_by"` // true if WE enabled it (it had been disabled)
}

// MikrotikSSHKey generates an ed25519 keypair, installs the public key for `user` over the
// RouterOS API, ensures the SSH service is on, and returns the private key. Idempotent-ish:
// re-running installs another key (RouterOS allows multiple per user).
func MikrotikSSHKey(host string, port int, user, pass string, timeout time.Duration) (*MikrotikResult, error) {
	if port == 0 {
		port = 8728
	}
	if timeout <= 0 {
		timeout = 20 * time.Second
	}
	addr := fmt.Sprintf("%s:%d", host, port)

	var (
		c   *routeros.Client
		err error
	)
	if port == 8729 {
		c, err = routeros.DialTLS(addr, user, pass, &tls.Config{InsecureSkipVerify: true})
	} else {
		c, err = routeros.DialTimeout(addr, user, pass, timeout)
	}
	if err != nil {
		return nil, fmt.Errorf("connect to RouterOS API: %w", err)
	}
	defer c.Close()

	// 1. keypair.
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("generate key: %w", err)
	}
	sshPub, err := ssh.NewPublicKey(pub)
	if err != nil {
		return nil, err
	}
	authLine := strings.TrimSpace(string(ssh.MarshalAuthorizedKey(sshPub))) + " rusted"

	pemBlock, err := ssh.MarshalPrivateKey(priv, "rusted@"+host)
	if err != nil {
		return nil, fmt.Errorf("marshal private key: %w", err)
	}
	privPEM := string(pem.EncodeToMemory(pemBlock))

	// 2. push the public key: write a small file (well under the 4KB API limit), import it
	//    for the user, then remove the file.
	fileName := "rusted-" + sanitize(user) + ".pub"
	removeFile(c, fileName) // clear any leftover from a previous run (RouterOS won't overwrite)
	if err := ros(c, "/file/add", "=name="+fileName, "=contents="+authLine); err != nil {
		return nil, permError("write public-key file", err)
	}
	if err := ros(c, "/user/ssh-keys/import", "=user="+user, "=public-key-file="+fileName); err != nil {
		removeFile(c, fileName)
		return nil, permError("import the SSH key (the API user needs the 'write'/'policy' permission)", err)
	}
	removeFile(c, fileName)

	// 3. make sure SSH is reachable, enabling it if it was off.
	res := &MikrotikResult{User: user, PrivateKey: privPEM, SSHPort: 22, SSHEnabled: true}
	if reply, err := c.RunArgs([]string{"/ip/service/print", "?name=ssh"}); err == nil && len(reply.Re) > 0 {
		m := reply.Re[0].Map
		if p, e := strconv.Atoi(m["port"]); e == nil && p > 0 {
			res.SSHPort = p
		}
		if m["disabled"] == "true" {
			res.SSHEnabled = false
			if id := m[".id"]; id != "" {
				if err := ros(c, "/ip/service/set", "=.id="+id, "=disabled=no"); err == nil {
					res.SSHEnabled = true
					res.SSHEnabledBy = true
				}
			}
		}
	}
	return res, nil
}

// ros runs a RouterOS API sentence and discards the reply, returning only the error.
func ros(c *routeros.Client, words ...string) error {
	_, err := c.RunArgs(words)
	return err
}

// removeFile deletes a file by name. RouterOS action commands don't take a `?name=` query,
// so we look the file up to get its .id first, then remove by id. Best-effort.
func removeFile(c *routeros.Client, name string) {
	reply, err := c.RunArgs([]string{"/file/print", "?name=" + name})
	if err != nil {
		return
	}
	for _, s := range reply.Re {
		if id := s.Map[".id"]; id != "" {
			_ = ros(c, "/file/remove", "=.id="+id)
		}
	}
}

func permError(what string, err error) error {
	m := err.Error()
	if strings.Contains(m, "not enough permissions") || strings.Contains(m, "not allowed") {
		return fmt.Errorf("%s: the API user lacks permission (grant it write/policy): %w", what, err)
	}
	return fmt.Errorf("%s: %w", what, err)
}

// sanitize keeps a RouterOS filename tame (alnum, dash, underscore).
func sanitize(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	if b.Len() == 0 {
		return "user"
	}
	return b.String()
}
