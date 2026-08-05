package fileutil

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestReadAndRecoverRestoresValidBackup(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	validate := func(data []byte) error {
		var value map[string]string
		return json.Unmarshal(data, &value)
	}

	first := []byte(`{"value":"first"}`)
	second := []byte(`{"value":"second"}`)
	if err := AtomicWriteFileWithValidator(path, first, 0o600, validate); err != nil {
		t.Fatalf("write first value: %v", err)
	}
	if err := AtomicWriteFileWithValidator(path, second, 0o600, validate); err != nil {
		t.Fatalf("write second value: %v", err)
	}
	backup, err := os.ReadFile(path + ".bak")
	if err != nil {
		t.Fatalf("read backup: %v", err)
	}
	if string(backup) != string(first) {
		t.Fatalf("backup = %q, want %q", backup, first)
	}
	if err := os.WriteFile(path, []byte("{"), 0o600); err != nil {
		t.Fatalf("corrupt primary: %v", err)
	}

	recovered, ok, err := ReadAndRecover(path, 0o600, validate)
	if err != nil {
		t.Fatalf("recover state: %v", err)
	}
	if !ok || string(recovered) != string(first) {
		t.Fatalf("recovered = (%q, %v), want (%q, true)", recovered, ok, first)
	}
	primary, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read restored primary: %v", err)
	}
	if string(primary) != string(first) {
		t.Fatalf("restored primary = %q, want %q", primary, first)
	}
}
