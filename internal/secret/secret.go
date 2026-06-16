// Package secret provides optional encryption-at-rest for sensitive
// credential fields stored in the SQLite database.
//
// If the RUSTED_SECRET environment variable is set, its value is used to
// derive an AES-256-GCM key (via SHA-256) and all sealed values are
// encrypted. If it is not set, values are stored in plaintext and a stored
// value is returned unchanged. Sealed values are prefixed with "enc:" and
// base64-encoded so the two modes can coexist in one database.
package secret

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"io"
	"os"
	"strings"
)

const prefix = "enc:"

// EnvKey is the environment variable that holds the master secret.
const EnvKey = "RUSTED_SECRET"

func key() ([]byte, bool) {
	v := os.Getenv(EnvKey)
	if v == "" {
		return nil, false
	}
	sum := sha256.Sum256([]byte(v))
	return sum[:], true
}

// Enabled reports whether encryption-at-rest is active.
func Enabled() bool {
	_, ok := key()
	return ok
}

// Seal encrypts plain if a master secret is configured, otherwise returns it
// unchanged.
func Seal(plain string) (string, error) {
	k, ok := key()
	if !ok {
		return plain, nil
	}
	block, err := aes.NewCipher(k)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}
	ct := gcm.Seal(nonce, nonce, []byte(plain), nil)
	return prefix + base64.StdEncoding.EncodeToString(ct), nil
}

// Open decrypts a value produced by Seal. Plaintext values (no prefix) are
// returned unchanged.
func Open(stored string) (string, error) {
	if !strings.HasPrefix(stored, prefix) {
		return stored, nil
	}
	k, ok := key()
	if !ok {
		return "", errors.New("value is encrypted but " + EnvKey + " is not set")
	}
	raw, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(stored, prefix))
	if err != nil {
		return "", err
	}
	block, err := aes.NewCipher(k)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	ns := gcm.NonceSize()
	if len(raw) < ns {
		return "", errors.New("ciphertext too short")
	}
	nonce, ct := raw[:ns], raw[ns:]
	pt, err := gcm.Open(nil, nonce, ct, nil)
	if err != nil {
		return "", err
	}
	return string(pt), nil
}
