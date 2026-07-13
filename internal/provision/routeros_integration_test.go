package provision

import (
	"bytes"
	"os"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"
)

// TestMikrotikSSHKeyIntegration runs the real bootstrap against a live RouterOS device and
// then proves a key-based SSH backup works. Skipped unless ROS_HOST/ROS_USER/ROS_PASS are
// set (so `go test ./...` stays hermetic in CI).
func TestMikrotikSSHKeyIntegration(t *testing.T) {
	host := os.Getenv("ROS_HOST")
	user := os.Getenv("ROS_USER")
	pass := os.Getenv("ROS_PASS")
	if host == "" || user == "" {
		t.Skip("set ROS_HOST / ROS_USER / ROS_PASS to run the live RouterOS bootstrap test")
	}

	res, err := MikrotikSSHKey(host, 8728, user, pass, 20*time.Second)
	if err != nil {
		t.Fatalf("provision: %v", err)
	}
	t.Logf("provisioned: user=%s ssh_port=%d ssh_enabled=%v enabled_by_us=%v key_len=%d",
		res.User, res.SSHPort, res.SSHEnabled, res.SSHEnabledBy, len(res.PrivateKey))
	if len(res.PrivateKey) == 0 {
		t.Fatal("no private key returned")
	}
	if !res.SSHEnabled {
		t.Fatal("ssh still not enabled after provisioning")
	}

	// Now connect over SSH with the generated key and pull a config - the whole point.
	signer, err := ssh.ParsePrivateKey([]byte(res.PrivateKey))
	if err != nil {
		t.Fatalf("parse generated private key: %v", err)
	}
	cfg := &ssh.ClientConfig{
		User:            user,
		Auth:            []ssh.AuthMethod{ssh.PublicKeys(signer)},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         15 * time.Second,
	}
	addr := host + ":" + itoa(res.SSHPort)
	client, err := ssh.Dial("tcp", addr, cfg)
	if err != nil {
		t.Fatalf("ssh dial with generated key (%s): %v", addr, err)
	}
	defer client.Close()

	sess, err := client.NewSession()
	if err != nil {
		t.Fatalf("ssh session: %v", err)
	}
	defer sess.Close()
	var out bytes.Buffer
	sess.Stdout = &out
	if err := sess.Run("/export terse"); err != nil {
		t.Fatalf("run /export over ssh: %v", err)
	}
	cfgText := out.String()
	if !strings.Contains(cfgText, "RouterOS") && len(cfgText) < 200 {
		t.Fatalf("export looks empty/short over key-based SSH:\n%s", cfgText)
	}
	t.Logf("key-based SSH /export ok: %d bytes", len(cfgText))
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}
