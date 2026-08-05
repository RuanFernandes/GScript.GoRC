package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync/atomic"
	"time"
)

const maxPluginMonacoContextBytes = 512 << 10

type PluginMonacoLanguage struct {
	ID         string   `json:"id"`
	PluginID   string   `json:"pluginId"`
	Extensions []string `json:"extensions,omitempty"`
	Aliases    []string `json:"aliases,omitempty"`
}

func pluginMonacoLanguageKey(pluginID, languageID string) string {
	return pluginID + "\x00" + languageID
}

func clonePluginMonacoLanguage(language PluginMonacoLanguage) PluginMonacoLanguage {
	language.Extensions = append([]string(nil), language.Extensions...)
	language.Aliases = append([]string(nil), language.Aliases...)
	return language
}

func validatePluginMonacoLanguage(language PluginMonacoLanguage) error {
	language.ID = strings.TrimSpace(language.ID)
	if language.ID == "" || len(language.ID) > 64 || strings.ContainsAny(language.ID, "\r\n/\\") {
		return errors.New("Monaco language id is invalid")
	}
	if len(language.Extensions) > 32 || len(language.Aliases) > 32 {
		return errors.New("Monaco language metadata has too many entries")
	}
	for _, extension := range language.Extensions {
		extension = strings.TrimSpace(extension)
		if extension == "" || len(extension) > 32 || !strings.HasPrefix(extension, ".") || strings.ContainsAny(extension, "/\\\r\n") {
			return errors.New("Monaco language extensions must be dot-prefixed")
		}
	}
	for _, alias := range language.Aliases {
		if strings.TrimSpace(alias) == "" || len(alias) > 64 || strings.ContainsAny(alias, "\r\n") {
			return errors.New("Monaco language aliases are invalid")
		}
	}
	return nil
}

func (a *App) PluginMonacoLanguageRegister(pluginID string, language PluginMonacoLanguage) error {
	if a.plugins == nil {
		return errors.New("plugin manager is unavailable")
	}
	if err := a.plugins.RequireAPI(pluginID, "monaco"); err != nil {
		return err
	}
	if err := validatePluginMonacoLanguage(language); err != nil {
		return err
	}
	language.ID = strings.TrimSpace(language.ID)
	language.PluginID = pluginID
	a.pluginMonacoLanguagesMu.Lock()
	if a.pluginMonacoLanguages == nil {
		a.pluginMonacoLanguages = map[string]PluginMonacoLanguage{}
	}
	a.pluginMonacoLanguages[pluginMonacoLanguageKey(pluginID, language.ID)] = clonePluginMonacoLanguage(language)
	a.pluginMonacoLanguagesMu.Unlock()
	a.emitPluginMonacoLanguagesChanged()
	return nil
}

func (a *App) PluginMonacoLanguageUnregister(pluginID, languageID string) error {
	if a.plugins == nil {
		return errors.New("plugin manager is unavailable")
	}
	if err := a.plugins.RequireAPI(pluginID, "monaco"); err != nil {
		return err
	}
	a.pluginMonacoLanguagesMu.Lock()
	delete(a.pluginMonacoLanguages, pluginMonacoLanguageKey(pluginID, strings.TrimSpace(languageID)))
	a.pluginMonacoLanguagesMu.Unlock()
	a.emitPluginMonacoLanguagesChanged()
	return nil
}

func (a *App) PluginMonacoLanguageCloseAll(pluginID string) error {
	if a.plugins == nil {
		return errors.New("plugin manager is unavailable")
	}
	if err := a.plugins.RequireAPI(pluginID, "monaco"); err != nil {
		return err
	}
	a.pluginMonacoLanguagesMu.Lock()
	for key, language := range a.pluginMonacoLanguages {
		if language.PluginID == pluginID {
			delete(a.pluginMonacoLanguages, key)
		}
	}
	a.pluginMonacoLanguagesMu.Unlock()
	a.emitPluginMonacoLanguagesChanged()
	return nil
}

func (a *App) emitPluginMonacoLanguagesChanged() {
	if a.app != nil {
		a.app.Event.Emit("plugin:monaco-languages")
	}
}

func (a *App) GetPluginMonacoLanguages() []PluginMonacoLanguage {
	active := map[string]bool{}
	if a.plugins != nil {
		for _, plugin := range a.plugins.List() {
			approved := false
			for _, api := range plugin.ApprovedAPIs {
				if api == "monaco" {
					approved = true
					break
				}
			}
			active[plugin.Manifest.ID] = plugin.Enabled && plugin.Status == "ready" && approved
		}
	}
	a.pluginMonacoLanguagesMu.RLock()
	values := make([]PluginMonacoLanguage, 0, len(a.pluginMonacoLanguages))
	for _, language := range a.pluginMonacoLanguages {
		if !active[language.PluginID] {
			continue
		}
		values = append(values, clonePluginMonacoLanguage(language))
	}
	a.pluginMonacoLanguagesMu.RUnlock()
	sort.Slice(values, func(i, j int) bool {
		if values[i].ID != values[j].ID {
			return values[i].ID < values[j].ID
		}
		return values[i].PluginID < values[j].PluginID
	})
	return values
}

type pluginMonacoRequest struct {
	RequestID string
	Kind      string
	Language  string
	Context   any
	Result    chan pluginMonacoResult
}

type pluginMonacoResult struct {
	Value any
	Err   string
}

type pluginMonacoEvent struct {
	RequestID string `json:"requestId"`
	Kind      string `json:"kind"`
	Language  string `json:"language"`
	Context   any    `json:"context"`
}

func validatePluginMonacoInput(kind, language string, context any) error {
	if kind != "diagnostics" && kind != "completions" {
		return errors.New("unsupported Monaco provider kind")
	}
	language = strings.TrimSpace(language)
	if language == "" || len(language) > 96 || strings.ContainsAny(language, "\r\n") {
		return errors.New("Monaco language is required")
	}
	b, err := json.Marshal(context)
	if err != nil {
		return errors.New("Monaco provider context must be JSON serializable")
	}
	if len(b) > maxPluginMonacoContextBytes {
		return errors.New("Monaco provider context exceeds 512 KiB")
	}
	return nil
}

// PluginMonacoRequest lets editor windows ask the main plugin runtime for a
// diagnostics/completions response. The runtime is the only component that
// knows which sandbox owns a provider, so the editor window never receives a
// plugin iframe or a Wails binding.
func (a *App) PluginMonacoRequest(kind, language string, context any) (any, error) {
	if err := validatePluginMonacoInput(kind, language, context); err != nil {
		return nil, err
	}
	if a.app == nil {
		return []any{}, nil
	}
	requestID := fmt.Sprintf("monaco-%d", atomic.AddUint64(&a.pluginMonacoSeq, 1))
	waiter := pluginMonacoRequest{
		RequestID: requestID,
		Kind:      kind,
		Language:  strings.TrimSpace(language),
		Context:   context,
		Result:    make(chan pluginMonacoResult, 1),
	}
	a.pluginMonacoMu.Lock()
	if a.pluginMonacoWaiters == nil {
		a.pluginMonacoWaiters = map[string]pluginMonacoRequest{}
	}
	a.pluginMonacoWaiters[requestID] = waiter
	a.pluginMonacoMu.Unlock()

	payload, err := json.Marshal(pluginMonacoEvent{
		RequestID: requestID,
		Kind:      kind,
		Language:  strings.TrimSpace(language),
		Context:   context,
	})
	if err != nil {
		a.removePluginMonacoWaiter(requestID)
		return nil, err
	}
	a.app.Event.Emit("plugin:monaco-request", string(payload))

	timer := time.NewTimer(2500 * time.Millisecond)
	defer timer.Stop()
	select {
	case result := <-waiter.Result:
		if result.Err != "" {
			return nil, errors.New(result.Err)
		}
		if result.Value == nil {
			return []any{}, nil
		}
		return result.Value, nil
	case <-timer.C:
		a.removePluginMonacoWaiter(requestID)
		return []any{}, nil
	}
}

func (a *App) removePluginMonacoWaiter(requestID string) (pluginMonacoRequest, bool) {
	a.pluginMonacoMu.Lock()
	defer a.pluginMonacoMu.Unlock()
	waiter, ok := a.pluginMonacoWaiters[requestID]
	if ok {
		delete(a.pluginMonacoWaiters, requestID)
	}
	return waiter, ok
}

// PluginMonacoResult is called by the main plugin runtime after a sandbox has
// answered a cross-window editor request. Empty pluginID means that no
// matching provider was registered and is accepted only for an existing
// request.
func (a *App) PluginMonacoResult(requestID, pluginID string, value any, errorMessage string) error {
	if strings.TrimSpace(requestID) == "" || len(requestID) > 128 || len(errorMessage) > 4096 {
		return errors.New("invalid Monaco response")
	}
	if encoded, err := json.Marshal(value); err != nil {
		return errors.New("Monaco provider response must be JSON serializable")
	} else if len(encoded) > maxPluginMonacoContextBytes {
		return errors.New("Monaco provider response exceeds 512 KiB")
	}
	if pluginID != "" {
		if a.plugins == nil {
			return errors.New("plugin manager is unavailable")
		}
		if err := a.plugins.RequireAPI(pluginID, "monaco"); err != nil {
			return err
		}
	}
	waiter, ok := a.removePluginMonacoWaiter(requestID)
	if !ok {
		return errors.New("Monaco request is no longer active")
	}
	waiter.Result <- pluginMonacoResult{Value: value, Err: strings.TrimSpace(errorMessage)}
	return nil
}
