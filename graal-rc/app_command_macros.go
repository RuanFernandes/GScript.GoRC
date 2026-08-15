package main

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"graal-rc/internal/fileutil"
)

const (
	commandMacroMaxCount         = 50
	commandMacroMaxNameLength    = 80
	commandMacroMaxCommandLength = 2000
	commandMacroMaxParameters    = 8
	commandMacroMaxParameterName = 40
	commandMacroMaxIDLength      = 128
	commandMacroParameterText    = "text"
	commandMacroParameterNumber  = "number"
	commandMacroParameterBoolean = "boolean"
)

var commandMacroParameterTypes = map[string]struct{}{
	commandMacroParameterText:    {},
	commandMacroParameterNumber:  {},
	commandMacroParameterBoolean: {},
}

// CommandMacroParameter is a typed argument accepted by a command macro.
type CommandMacroParameter struct {
	Name string `json:"name"`
	Type string `json:"type"`
}

// CommandMacro is a user-authored command shortcut. It is persisted locally,
// keyed by server name, and never sent to the Graal server.
type CommandMacro struct {
	ID         string                  `json:"id"`
	Name       string                  `json:"name"`
	Command    string                  `json:"command"`
	Parameters []CommandMacroParameter `json:"parameters,omitempty"`
	CreatedAt  int64                   `json:"createdAt"`
	UpdatedAt  int64                   `json:"updatedAt"`
}

// CommandMacroStore tells the frontend whether the server has an explicit
// entry. Exists is important for distinguishing "saved empty" from "not yet
// migrated", so old localStorage macros are imported exactly once.
type CommandMacroStore struct {
	Macros []CommandMacro `json:"macros"`
	Exists bool           `json:"exists"`
}

type commandMacroFile struct {
	Servers map[string][]CommandMacro `json:"servers"`
}

func commandMacrosPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "graal-rc", "command-macros.json"), nil
}

func commandMacroServerKey(serverName string) string {
	serverName = strings.TrimSpace(serverName)
	if serverName == "" {
		return "default"
	}
	return strings.ToLower(serverName)
}

func loadCommandMacroFile(path string) (map[string][]CommandMacro, error) {
	data, ok, err := fileutil.ReadAndRecover(path, 0o600, func(data []byte) error {
		_, err := decodeCommandMacroFile(data)
		return err
	})
	if err != nil {
		return nil, err
	}
	if !ok {
		return map[string][]CommandMacro{}, nil
	}
	return decodeCommandMacroFile(data)
}

func decodeCommandMacroFile(data []byte) (map[string][]CommandMacro, error) {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, err
	}
	if raw == nil {
		return nil, errors.New("command macro file must be a JSON object")
	}
	serversRaw, ok := raw["servers"]
	if !ok {
		return nil, errors.New("command macro file is missing servers")
	}
	var servers map[string][]CommandMacro
	if err := json.Unmarshal(serversRaw, &servers); err != nil {
		return nil, fmt.Errorf("servers: %w", err)
	}
	if servers == nil {
		return nil, errors.New("servers must be a JSON object")
	}

	cleaned := make(map[string][]CommandMacro, len(servers))
	for serverName, macros := range servers {
		key := commandMacroServerKey(serverName)
		if _, exists := cleaned[key]; exists {
			return nil, fmt.Errorf("duplicate server key %q", key)
		}
		clean, err := normalizeCommandMacroList(macros)
		if err != nil {
			return nil, fmt.Errorf("server %q: %w", serverName, err)
		}
		cleaned[key] = clean
	}
	return cleaned, nil
}

func persistCommandMacroFile(path string, servers map[string][]CommandMacro) error {
	if servers == nil {
		servers = map[string][]CommandMacro{}
	}
	normalizedServers := make(map[string][]CommandMacro, len(servers))
	for serverName, macros := range servers {
		key := commandMacroServerKey(serverName)
		if _, exists := normalizedServers[key]; exists {
			return fmt.Errorf("duplicate server key %q", key)
		}
		clean, err := normalizeCommandMacroList(macros)
		if err != nil {
			return fmt.Errorf("server %q: %w", serverName, err)
		}
		normalizedServers[key] = clean
	}
	data, err := json.MarshalIndent(commandMacroFile{Servers: normalizedServers}, "", "  ")
	if err != nil {
		return err
	}
	return fileutil.AtomicWriteFileWithValidator(path, data, 0o600, func(current []byte) error {
		_, err := decodeCommandMacroFile(current)
		return err
	})
}

func normalizeCommandMacroList(macros []CommandMacro) ([]CommandMacro, error) {
	if len(macros) > commandMacroMaxCount {
		return nil, fmt.Errorf("maximum of %d macros exceeded", commandMacroMaxCount)
	}
	clean := make([]CommandMacro, 0, len(macros))
	seenIDs := make(map[string]struct{}, len(macros))
	seenNames := make(map[string]struct{}, len(macros))
	for index, macro := range macros {
		normalized, err := normalizeCommandMacro(macro)
		if err != nil {
			return nil, fmt.Errorf("macro %d: %w", index, err)
		}
		if _, exists := seenIDs[normalized.ID]; exists {
			return nil, fmt.Errorf("macro %q has a duplicate id", normalized.Name)
		}
		nameKey := strings.ToLower(normalized.Name)
		if _, exists := seenNames[nameKey]; exists {
			return nil, fmt.Errorf("macro name %q is duplicated", normalized.Name)
		}
		seenIDs[normalized.ID] = struct{}{}
		seenNames[nameKey] = struct{}{}
		clean = append(clean, normalized)
	}
	return clean, nil
}

func normalizeCommandMacro(macro CommandMacro) (CommandMacro, error) {
	macro.ID = strings.TrimSpace(macro.ID)
	if macro.ID == "" || len(macro.ID) > commandMacroMaxIDLength {
		return CommandMacro{}, fmt.Errorf("id is required and must be at most %d characters", commandMacroMaxIDLength)
	}
	macro.Name = strings.TrimSpace(macro.Name)
	if macro.Name == "" || len(macro.Name) > commandMacroMaxNameLength {
		return CommandMacro{}, fmt.Errorf("name is required and must be at most %d characters", commandMacroMaxNameLength)
	}
	macro.Command = strings.TrimSpace(macro.Command)
	if macro.Command == "" || len(macro.Command) > commandMacroMaxCommandLength {
		return CommandMacro{}, fmt.Errorf("command is required and must be at most %d characters", commandMacroMaxCommandLength)
	}
	if len(macro.Parameters) > commandMacroMaxParameters {
		return CommandMacro{}, fmt.Errorf("maximum of %d parameters exceeded", commandMacroMaxParameters)
	}
	parameters := make([]CommandMacroParameter, 0, len(macro.Parameters))
	seenParameters := make(map[string]struct{}, len(macro.Parameters))
	for index, parameter := range macro.Parameters {
		parameter.Name = strings.TrimSpace(parameter.Name)
		if parameter.Name == "" || len(parameter.Name) > commandMacroMaxParameterName {
			return CommandMacro{}, fmt.Errorf("parameter %d name is required and must be at most %d characters", index, commandMacroMaxParameterName)
		}
		if !isValidCommandMacroParameterName(parameter.Name) {
			return CommandMacro{}, fmt.Errorf("parameter %q has an invalid name", parameter.Name)
		}
		if _, ok := commandMacroParameterTypes[parameter.Type]; !ok {
			return CommandMacro{}, fmt.Errorf("parameter %q has an invalid type", parameter.Name)
		}
		parameterKey := strings.ToLower(parameter.Name)
		if _, exists := seenParameters[parameterKey]; exists {
			return CommandMacro{}, fmt.Errorf("parameter %q is duplicated", parameter.Name)
		}
		seenParameters[parameterKey] = struct{}{}
		parameters = append(parameters, parameter)
	}
	if len(parameters) == 0 {
		macro.Parameters = nil
	} else {
		macro.Parameters = parameters
	}
	if macro.CreatedAt < 0 || macro.UpdatedAt < 0 {
		return CommandMacro{}, errors.New("timestamps cannot be negative")
	}
	if macro.UpdatedAt < macro.CreatedAt {
		return CommandMacro{}, errors.New("updatedAt cannot be earlier than createdAt")
	}
	return macro, nil
}

func isValidCommandMacroParameterName(name string) bool {
	if name == "" {
		return false
	}
	for index, char := range name {
		if (char >= 'A' && char <= 'Z') || (char >= 'a' && char <= 'z') || (index > 0 && ((char >= '0' && char <= '9') || char == '_' || char == '-')) {
			continue
		}
		return false
	}
	return true
}

func newCommandMacroID() (string, error) {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", fmt.Errorf("generate macro id: %w", err)
	}
	return hex.EncodeToString(raw[:]), nil
}

// GetCommandMacros loads the macros for one server from the user's local
// configuration directory. The data is never uploaded to the server.
func (a *App) GetCommandMacros(serverName string) (CommandMacroStore, error) {
	path, err := commandMacrosPath()
	if err != nil {
		return CommandMacroStore{}, err
	}
	key := commandMacroServerKey(serverName)
	a.commandMacrosMu.Lock()
	defer a.commandMacrosMu.Unlock()
	servers, err := loadCommandMacroFile(path)
	if err != nil {
		return CommandMacroStore{}, fmt.Errorf("load command macros: %w", err)
	}
	macros, exists := servers[key]
	return CommandMacroStore{Macros: append([]CommandMacro(nil), macros...), Exists: exists}, nil
}

// SetCommandMacros replaces one server's macro list. It is used to migrate
// the previous localStorage data into the user-level file after an update.
func (a *App) SetCommandMacros(serverName string, macros []CommandMacro) error {
	clean, err := normalizeCommandMacroList(macros)
	if err != nil {
		return err
	}
	path, err := commandMacrosPath()
	if err != nil {
		return err
	}
	key := commandMacroServerKey(serverName)
	a.commandMacrosMu.Lock()
	defer a.commandMacrosMu.Unlock()
	servers, err := loadCommandMacroFile(path)
	if err != nil {
		return fmt.Errorf("load command macros: %w", err)
	}
	servers[key] = clean
	if err := persistCommandMacroFile(path, servers); err != nil {
		return fmt.Errorf("save command macros: %w", err)
	}
	return nil
}

// SaveCommandMacro creates or updates a macro by name for one server.
func (a *App) SaveCommandMacro(serverName, name, command string, parameters []CommandMacroParameter) (CommandMacro, error) {
	name = strings.TrimSpace(name)
	command = strings.TrimSpace(command)
	if name == "" || len(name) > commandMacroMaxNameLength {
		return CommandMacro{}, fmt.Errorf("macro name is required and must be at most %d characters", commandMacroMaxNameLength)
	}
	if command == "" || len(command) > commandMacroMaxCommandLength {
		return CommandMacro{}, fmt.Errorf("macro command is required and must be at most %d characters", commandMacroMaxCommandLength)
	}
	if len(parameters) > commandMacroMaxParameters {
		return CommandMacro{}, fmt.Errorf("maximum of %d parameters exceeded", commandMacroMaxParameters)
	}

	path, err := commandMacrosPath()
	if err != nil {
		return CommandMacro{}, err
	}
	key := commandMacroServerKey(serverName)
	a.commandMacrosMu.Lock()
	defer a.commandMacrosMu.Unlock()
	servers, err := loadCommandMacroFile(path)
	if err != nil {
		return CommandMacro{}, fmt.Errorf("load command macros: %w", err)
	}
	macros := servers[key]
	nameKey := strings.ToLower(name)
	now := time.Now().UnixMilli()
	var saved CommandMacro
	updated := false
	for index := range macros {
		if strings.ToLower(macros[index].Name) != nameKey {
			continue
		}
		saved = CommandMacro{
			ID:         macros[index].ID,
			Name:       name,
			Command:    command,
			Parameters: parameters,
			CreatedAt:  macros[index].CreatedAt,
			UpdatedAt:  now,
		}
		macros[index] = saved
		updated = true
		break
	}
	if !updated {
		if len(macros) >= commandMacroMaxCount {
			return CommandMacro{}, fmt.Errorf("maximum of %d macros reached", commandMacroMaxCount)
		}
		id, idErr := newCommandMacroID()
		if idErr != nil {
			return CommandMacro{}, idErr
		}
		saved = CommandMacro{
			ID:         id,
			Name:       name,
			Command:    command,
			Parameters: parameters,
			CreatedAt:  now,
			UpdatedAt:  now,
		}
		macros = append([]CommandMacro{saved}, macros...)
	}
	clean, err := normalizeCommandMacroList(macros)
	if err != nil {
		return CommandMacro{}, err
	}
	servers[key] = clean
	if err := persistCommandMacroFile(path, servers); err != nil {
		return CommandMacro{}, fmt.Errorf("save command macro: %w", err)
	}
	for _, macro := range clean {
		if macro.ID == saved.ID {
			return macro, nil
		}
	}
	return CommandMacro{}, errors.New("saved command macro was not found after normalization")
}

// DeleteCommandMacro removes one macro. Deleting the final macro keeps an
// explicit empty server entry so an older localStorage copy cannot reappear.
func (a *App) DeleteCommandMacro(serverName, id string) error {
	id = strings.TrimSpace(id)
	if id == "" {
		return errors.New("macro id is required")
	}
	path, err := commandMacrosPath()
	if err != nil {
		return err
	}
	key := commandMacroServerKey(serverName)
	a.commandMacrosMu.Lock()
	defer a.commandMacrosMu.Unlock()
	servers, err := loadCommandMacroFile(path)
	if err != nil {
		return fmt.Errorf("load command macros: %w", err)
	}
	macros := servers[key]
	filtered := make([]CommandMacro, 0, len(macros))
	for _, macro := range macros {
		if macro.ID != id {
			filtered = append(filtered, macro)
		}
	}
	servers[key] = filtered
	if err := persistCommandMacroFile(path, servers); err != nil {
		return fmt.Errorf("save command macros: %w", err)
	}
	return nil
}
