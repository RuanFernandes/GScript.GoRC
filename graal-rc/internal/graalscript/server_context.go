package graalscript

import (
	"bufio"
	"sort"
	"strings"
)

// ServerScriptContext contains the raw server-side configuration snapshots
// made available to the embedded language server after server login.
type ServerScriptContext struct {
	ServerOptions string
	ServerFlags   string
}

type serverContextDefinitions struct {
	server         []Definition
	serverReadable []Definition
	serverOptions  []Definition
}

func parseServerContext(context ServerScriptContext) serverContextDefinitions {
	return serverContextDefinitions{
		server:         serverFlagDefinitions(context.ServerFlags, "server.", "serverside", "Server flag; readable and writable on the server."),
		serverReadable: serverFlagDefinitions(context.ServerFlags, "serverr.", "server/client", "Server flag; writable on the server and readable on the client."),
		serverOptions:  serverOptionDefinitions(context.ServerOptions),
	}
}

func serverFlagDefinitions(text, prefix, scope, description string) []Definition {
	prefix = strings.ToLower(prefix)
	seen := map[string]bool{}
	definitions := []Definition{}
	for _, key := range configKeys(text) {
		lower := strings.ToLower(key)
		if !strings.HasPrefix(lower, prefix) || len(key) <= len(prefix) {
			continue
		}
		name := strings.TrimSpace(key[len(prefix):])
		if name == "" || seen[normalizeName(name)] {
			continue
		}
		seen[normalizeName(name)] = true
		definitions = append(definitions, Definition{
			Name:        name,
			Kind:        "variable",
			Scope:       scope,
			Description: description,
		})
	}
	sortDefinitions(definitions)
	return definitions
}

func serverOptionDefinitions(text string) []Definition {
	seen := map[string]bool{}
	definitions := []Definition{}
	for _, name := range configKeys(text) {
		name = strings.TrimSpace(name)
		if name == "" || seen[normalizeName(name)] {
			continue
		}
		seen[normalizeName(name)] = true
		definitions = append(definitions, Definition{
			Name:        name,
			Kind:        "variable",
			Scope:       "serverside",
			Description: "Server option; read-only from GraalScript.",
		})
	}
	sortDefinitions(definitions)
	return definitions
}

// configKeys extracts the key on each k=v line. Server options use comments
// and [section] headers; ignoring both here also keeps those labels out of
// serveroptions completion. The parser is deliberately tolerant of a flag
// line without an explicit value because server flags can be stored as bare
// names by older servers.
func configKeys(text string) []string {
	scanner := bufio.NewScanner(strings.NewReader(text))
	scanner.Buffer(make([]byte, 1024), 8<<20)
	keys := []string{}
	for scanner.Scan() {
		line := strings.TrimSpace(strings.TrimPrefix(scanner.Text(), "\ufeff"))
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "//") || strings.HasPrefix(line, "[") {
			continue
		}
		if comment := strings.IndexByte(line, '#'); comment >= 0 {
			line = strings.TrimSpace(line[:comment])
		}
		if line == "" {
			continue
		}
		if equals := strings.IndexByte(line, '='); equals >= 0 {
			line = strings.TrimSpace(line[:equals])
		} else if fields := strings.Fields(line); len(fields) > 0 {
			line = fields[0]
		}
		line = strings.Trim(strings.TrimSpace(line), "\"'")
		if line != "" {
			keys = append(keys, line)
		}
	}
	return keys
}

func sortDefinitions(definitions []Definition) {
	sort.SliceStable(definitions, func(i, j int) bool {
		return strings.ToLower(definitions[i].Name) < strings.ToLower(definitions[j].Name)
	})
}

func (s *LanguageServer) serverScopeDefinitions(receiver, side string) []Definition {
	var definitions []Definition
	switch strings.ToLower(strings.TrimSpace(receiver)) {
	case "server":
		if side == scriptSideServer {
			definitions = s.serverContext.server
		}
	case "serverr":
		definitions = s.serverContext.serverReadable
	case "serveroptions":
		if side == scriptSideServer {
			definitions = s.serverContext.serverOptions
		}
	default:
		return nil
	}
	return append([]Definition(nil), definitions...)
}

func serverScopeVisible(receiver, side string) bool {
	switch strings.ToLower(strings.TrimSpace(receiver)) {
	case "server", "serveroptions":
		return side == scriptSideServer
	case "serverr":
		return true
	default:
		return true
	}
}

func (s *LanguageServer) serverScopeDefinition(receiver, name, side string) (Definition, bool) {
	for _, definition := range s.serverScopeDefinitions(receiver, side) {
		if strings.EqualFold(definition.Name, name) {
			return definition, true
		}
	}
	return Definition{}, false
}
