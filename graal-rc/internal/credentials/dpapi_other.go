// SPDX-License-Identifier: LGPL-2.0-only
//go:build !windows

package credentials

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"
)

// errEmpty mirrors dpapi_windows.go for zero-length inputs.
var errEmpty = errors.New("dpapi: empty data")

// Windows binds the vault key to the OS user via DPAPI (CryptProtectData). There
// is no DPAPI equivalent on Linux/macOS, so a random AES-256 key is generated
// once and stored mode 0600 under the user config dir. This protects the vault
// at rest against casual reads but is NOT bound to an OS login secret the way
// DPAPI is — a user who can read the keyfile can decrypt the vault. Acceptable
// for a single-user RC client; the Windows build keeps full DPAPI protection.
func keyPath() (string, error) {
	base, err := os.UserConfigDir()
	if err != nil || base == "" {
		base, err = os.UserHomeDir()
		if err != nil {
			return "", err
		}
	}
	dir := filepath.Join(base, "graal-rc")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	return filepath.Join(dir, "vault.key"), nil
}

// loadKey returns the per-user master key, generating + persisting it on first
// use. The key is hex-encoded on disk so it is not raw bytes sitting in a file.
func loadKey() ([]byte, error) {
	p, err := keyPath()
	if err != nil {
		return nil, err
	}
	if raw, rerr := os.ReadFile(p); rerr == nil {
		if k, derr := hex.DecodeString(strings.TrimSpace(string(raw))); derr == nil && len(k) == 32 {
			return k, nil
		}
	}
	k := make([]byte, 32)
	if _, err := rand.Read(k); err != nil {
		return nil, err
	}
	if err := os.WriteFile(p, []byte(hex.EncodeToString(k)), 0o600); err != nil {
		return nil, err
	}
	return k, nil
}

// protect encrypts data with AES-GCM under the per-user key. Output layout is
// nonce(12) || ciphertext+tag, so unprotect splits the leading nonce off.
func protect(data []byte) ([]byte, error) {
	if len(data) == 0 {
		return nil, errEmpty
	}
	key, err := loadKey()
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	return gcm.Seal(nonce, nonce, data, nil), nil
}

// unprotect decrypts data produced by protect.
func unprotect(data []byte) ([]byte, error) {
	if len(data) == 0 {
		return nil, errEmpty
	}
	key, err := loadKey()
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	ns := gcm.NonceSize()
	if len(data) < ns+1 {
		return nil, errors.New("dpapi: ciphertext too short")
	}
	return gcm.Open(nil, data[:ns], data[ns:], nil)
}
