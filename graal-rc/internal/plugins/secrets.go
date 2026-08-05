package plugins

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"

	"graal-rc/internal/credentials"
	"graal-rc/internal/fileutil"
)

var secretsMu sync.Mutex

func (m *Manager) secretPath(id string) (string, error) {
	m.mu.RLock()
	_, ok := m.plugins[id]
	m.mu.RUnlock()
	if !ok {
		return "", ErrPluginNotFound
	}
	return filepath.Join(m.root, ".secrets", id+".dat"), nil
}

func (m *Manager) SecretGet(id, key string) (string, bool, error) {
	if key == "" {
		return "", false, errors.New("secret key is required")
	}
	path, err := m.secretPath(id)
	if err != nil {
		return "", false, err
	}
	secretsMu.Lock()
	defer secretsMu.Unlock()
	values, err := readSecrets(path)
	if err != nil {
		return "", false, err
	}
	value, ok := values[key]
	return value, ok, nil
}

func (m *Manager) SecretSet(id, key, value string) error {
	if key == "" {
		return errors.New("secret key is required")
	}
	if len(value) > 1<<20 {
		return errors.New("secret value exceeds 1 MiB")
	}
	path, err := m.secretPath(id)
	if err != nil {
		return err
	}
	secretsMu.Lock()
	defer secretsMu.Unlock()
	values, err := readSecrets(path)
	if err != nil {
		return err
	}
	values[key] = value
	return writeSecrets(path, values)
}

func (m *Manager) SecretDelete(id, key string) error {
	path, err := m.secretPath(id)
	if err != nil {
		return err
	}
	secretsMu.Lock()
	defer secretsMu.Unlock()
	values, err := readSecrets(path)
	if err != nil {
		return err
	}
	delete(values, key)
	return writeSecrets(path, values)
}

func readSecrets(path string) (map[string]string, error) {
	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return map[string]string{}, nil
	}
	if err != nil {
		return nil, err
	}
	plain, err := credentials.Unprotect(b)
	if err != nil {
		return nil, err
	}
	values := map[string]string{}
	if err := json.Unmarshal(plain, &values); err != nil {
		return nil, err
	}
	return values, nil
}

func writeSecrets(path string, values map[string]string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	plain, err := json.Marshal(values)
	if err != nil {
		return err
	}
	ciphertext, err := credentials.Protect(plain)
	if err != nil {
		return err
	}
	return fileutil.AtomicWriteFile(path, ciphertext, 0o600)
}
