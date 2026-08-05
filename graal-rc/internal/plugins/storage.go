package plugins

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"

	"graal-rc/internal/fileutil"
)

type storage struct {
	Values map[string]string `json:"values"`
}

var storageMu sync.Mutex

func (m *Manager) StorageGet(id, key string) (string, bool, error) {
	if key == "" {
		return "", false, errors.New("storage key is required")
	}
	path, err := m.KVPath(id)
	if err != nil {
		return "", false, err
	}
	storageMu.Lock()
	defer storageMu.Unlock()
	s, err := readStorage(path)
	if err != nil {
		return "", false, err
	}
	value, ok := s.Values[key]
	return value, ok, nil
}

func (m *Manager) StorageSet(id, key, value string) error {
	if key == "" {
		return errors.New("storage key is required")
	}
	if len(value) > 1<<20 {
		return errors.New("storage value exceeds 1 MiB")
	}
	path, err := m.KVPath(id)
	if err != nil {
		return err
	}
	storageMu.Lock()
	defer storageMu.Unlock()
	s, err := readStorage(path)
	if err != nil {
		return err
	}
	s.Values[key] = value
	return writeStorage(path, s)
}

func (m *Manager) StorageDelete(id, key string) error {
	path, err := m.KVPath(id)
	if err != nil {
		return err
	}
	storageMu.Lock()
	defer storageMu.Unlock()
	s, err := readStorage(path)
	if err != nil {
		return err
	}
	delete(s.Values, key)
	return writeStorage(path, s)
}

func readStorage(path string) (storage, error) {
	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return storage{Values: map[string]string{}}, nil
	}
	if err != nil {
		return storage{}, err
	}
	var s storage
	if err := json.Unmarshal(b, &s); err != nil {
		return storage{}, err
	}
	if s.Values == nil {
		s.Values = map[string]string{}
	}
	return s, nil
}

func writeStorage(path string, s storage) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return fileutil.AtomicWriteFileWithValidator(path, b, 0o600, func(payload []byte) error {
		var current storage
		return json.Unmarshal(payload, &current)
	})
}
