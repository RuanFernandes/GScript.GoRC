package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"graal-rc/internal/fileutil"
	synclib "graal-rc/internal/sync"
)

func TestSyncConfigPersistenceRecoversCorruptPrimary(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sync.json")
	first := map[string]synclib.SyncConfig{
		"Zodiac": {Enabled: true, OutputDir: `C:\sync\zodiac`, PollingMinutes: 5},
	}
	second := map[string]synclib.SyncConfig{
		"Era": {Enabled: true, OutputDir: `C:\sync\era`, PollingMinutes: 10},
	}
	encode := func(configs map[string]synclib.SyncConfig) []byte {
		data, err := json.Marshal(syncFile{Servers: configs})
		if err != nil {
			t.Fatalf("marshal sync config: %v", err)
		}
		return data
	}
	validate := func(data []byte) error {
		_, err := decodeSyncConfigs(data)
		return err
	}
	if err := fileutil.AtomicWriteFileWithValidator(path, encode(first), 0o600, validate); err != nil {
		t.Fatalf("write first sync config: %v", err)
	}
	if err := fileutil.AtomicWriteFileWithValidator(path, encode(second), 0o600, validate); err != nil {
		t.Fatalf("write second sync config: %v", err)
	}
	if err := os.WriteFile(path, []byte("{"), 0o600); err != nil {
		t.Fatalf("corrupt sync config: %v", err)
	}

	got, err := loadSyncCfgsFromPath(path)
	if err != nil {
		t.Fatalf("load recovered sync config: %v", err)
	}
	if got["Zodiac"] != first["Zodiac"] || len(got) != 1 {
		t.Fatalf("recovered sync config = %+v, want %+v", got, first)
	}
}

func TestDecodeSyncConfigsSupportsLegacySingleConfig(t *testing.T) {
	data := []byte(`{"enabled":true,"outputDir":"scripts","pollingMinutes":0}`)
	got, err := decodeSyncConfigs(data)
	if err != nil {
		t.Fatalf("decode legacy sync config: %v", err)
	}
	if got[""].PollingMinutes != 5 || !got[""].Enabled {
		t.Fatalf("decoded legacy config = %+v", got[""])
	}
}
