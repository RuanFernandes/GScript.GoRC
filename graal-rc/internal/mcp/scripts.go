package mcp

import (
	"math"
	"strconv"
	"strings"
)

// ScriptBackend is the NC script surface exposed through MCP. Deleting
// weapons, classes or NPCs is deliberately absent: an agent can create and
// change scripts, but removing one stays a manual action in the desktop client.
type ScriptBackend interface {
	// ListScripts returns the scripts this account can read. kind is "weapon",
	// "class", "npc" or "" for all three.
	ListScripts(kind string) ([]ScriptEntry, error)
	ReadScript(ref ScriptRef) (Script, error)
	// WriteScript replaces the content of an existing script.
	WriteScript(ref ScriptRef, script string) (Script, error)
	// AddScript creates a weapon, class or NPC that does not exist yet.
	AddScript(spec NewScript) (Script, error)
	// RefreshScripts asks the NC server for the weapon list again.
	RefreshScripts() error

	ResetNPC(ref ScriptRef) error
	WarpNPC(ref ScriptRef, level string, x, y float64) error
	ReadNPCFlags(ref ScriptRef) (string, error)
	WriteNPCFlags(ref ScriptRef, flags string) error
	ReadNPCAttributes(ref ScriptRef) (string, error)

	SyncStatus() SyncInfo
}

// ScriptRef identifies one script. Weapons and classes are addressed by Name;
// an NPC is addressed by ID when HasID is set, otherwise by its exact Name.
type ScriptRef struct {
	Type  string
	Name  string
	ID    int
	HasID bool
}

type ScriptEntry struct {
	Type    string `json:"type"`
	Name    string `json:"name"`
	ID      *int   `json:"id,omitempty"`
	NPCType string `json:"npcType,omitempty"`
	Level   string `json:"level,omitempty"`
}

type Script struct {
	Type   string `json:"type"`
	Name   string `json:"name"`
	ID     *int   `json:"id,omitempty"`
	Script string `json:"script"`
	// LocalPath is the file the sync engine keeps for this script, when sync is
	// enabled. Editing that file is equivalent to script_write.
	LocalPath string `json:"localPath,omitempty"`
}

// NewScript describes a script to create. The NPC-only fields are ignored for
// weapons and classes.
type NewScript struct {
	Type   string
	Name   string
	Script string

	ID       int
	HasID    bool
	NPCType  string
	Scripter string
	Level    string
	X        string
	Y        string
}

type SyncInfo struct {
	Enabled           bool   `json:"enabled"`
	Server            string `json:"server"`
	OutputDir         string `json:"outputDir"`
	AutoPushLocal     bool   `json:"autoPushLocal"`
	AutoPullServer    bool   `json:"autoPullServer"`
	Paused            bool   `json:"paused"`
	InitialSync       bool   `json:"initialSync"`
	PanicMode         bool   `json:"panicMode"`
	PanicReason       string `json:"panicReason,omitempty"`
	NCDown            bool   `json:"ncDown"`
	ScriptWriteAccess bool   `json:"scriptWriteAccess"`
	LastSyncAt        int64  `json:"lastSyncAt"`
	LastError         string `json:"lastError,omitempty"`
	Layout            string `json:"layout"`
}

// SyncLayout documents where the sync engine keeps each script kind.
const SyncLayout = "<outputDir>/weapons/<name>.gs2, <outputDir>/classes/<name>.gs2, <outputDir>/npcs/<name>.gs2; " +
	"characters that are not valid in a file name are written as %NNN (decimal code), for example Staff/News -> Staff%047News.gs2"

var (
	scriptTypeSchema = map[string]any{"type": "string", "enum": []string{"weapon", "class", "npc"}}
	npcIDSchema      = map[string]any{"type": "integer", "minimum": 0, "description": "ID do NPC. Preferível ao nome."}
	npcNameSchema    = map[string]any{"type": "string", "description": "Nome exato do NPC, usado quando id não é informado."}
)

func scriptToolDefinitions() []map[string]any {
	return []map[string]any{
		{"name": "script_list", "description": "Lista weapons, classes e NPCs que a conta pode ler. Filtra por tipo e por trecho do nome.", "inputSchema": objectSchema(map[string]any{
			"type":  scriptTypeSchema,
			"query": map[string]any{"type": "string", "description": "Trecho do nome, sem diferenciar maiúsculas."},
		})},
		{"name": "script_read", "description": "Lê o script de um weapon, classe ou NPC direto do servidor. Retorna também o arquivo local do sync, quando existe.", "inputSchema": objectSchema(map[string]any{
			"type": scriptTypeSchema,
			"name": map[string]any{"type": "string", "description": "Nome do weapon/classe, ou do NPC quando id não é informado."},
			"id":   npcIDSchema,
		}, "type")},
		{"name": "script_write", "description": "Substitui o script de um weapon, classe ou NPC que já existe. O servidor aplica na hora; confirme com get_rc_chat.", "inputSchema": objectSchema(map[string]any{
			"type":   scriptTypeSchema,
			"name":   map[string]any{"type": "string", "description": "Nome do weapon/classe, ou do NPC quando id não é informado."},
			"id":     npcIDSchema,
			"script": map[string]any{"type": "string", "description": "Conteúdo completo do script."},
		}, "type", "script")},
		{"name": "weapon_add", "description": "Cria um weapon novo, opcionalmente já com o script. Falha se o nome já existir.", "inputSchema": objectSchema(map[string]any{
			"name":   map[string]any{"type": "string", "minLength": 1},
			"script": map[string]any{"type": "string"},
		}, "name")},
		{"name": "class_add", "description": "Cria uma classe nova, opcionalmente já com o script. Falha se o nome já existir.", "inputSchema": objectSchema(map[string]any{
			"name":   map[string]any{"type": "string", "minLength": 1},
			"script": map[string]any{"type": "string"},
		}, "name")},
		{"name": "npc_add", "description": "Cria um NPC de banco (DB NPC) novo, opcionalmente já com o script. Sem id, usa o primeiro livre a partir de 1000.", "inputSchema": objectSchema(map[string]any{
			"name":     map[string]any{"type": "string", "minLength": 1},
			"id":       map[string]any{"type": "integer", "minimum": 0},
			"npcType":  map[string]any{"type": "string", "description": "Tipo do NPC; pode ficar vazio."},
			"scripter": map[string]any{"type": "string"},
			"level":    map[string]any{"type": "string", "description": "Level onde o NPC nasce, por exemplo onlinestartlocal.nw."},
			"x":        map[string]any{"type": "number", "default": 0},
			"y":        map[string]any{"type": "number", "default": 0},
			"script":   map[string]any{"type": "string"},
		}, "name")},
		{"name": "npc_reset", "description": "Reinicia um NPC.", "inputSchema": objectSchema(map[string]any{
			"id": npcIDSchema, "name": npcNameSchema,
		})},
		{"name": "npc_warp", "description": "Move um NPC para level/x/y.", "inputSchema": objectSchema(map[string]any{
			"id": npcIDSchema, "name": npcNameSchema,
			"level": map[string]any{"type": "string", "minLength": 1},
			"x":     map[string]any{"type": "number"},
			"y":     map[string]any{"type": "number"},
		}, "level", "x", "y")},
		{"name": "npc_flags_read", "description": "Lê as flags de um NPC.", "inputSchema": objectSchema(map[string]any{
			"id": npcIDSchema, "name": npcNameSchema,
		})},
		{"name": "npc_flags_write", "description": "Substitui as flags de um NPC.", "inputSchema": objectSchema(map[string]any{
			"id": npcIDSchema, "name": npcNameSchema,
			"flags": map[string]any{"type": "string", "description": "Texto completo das flags, uma por linha."},
		}, "flags")},
		{"name": "npc_attributes_read", "description": "Lê os atributos (somente leitura) de um NPC.", "inputSchema": objectSchema(map[string]any{
			"id": npcIDSchema, "name": npcNameSchema,
		})},
		{"name": "script_refresh", "description": "Pede a lista de weapons ao NC server de novo.", "inputSchema": objectSchema(map[string]any{})},
		{"name": "sync_status", "description": "Mostra o estado do sync de scripts: pasta local, auto push/pull, pausa e erros.", "inputSchema": objectSchema(map[string]any{})},
	}
}

// callScriptTool handles the script tools. handled is false when name is not
// one of them, so the caller can report an unknown tool.
func (s *Server) callScriptTool(name string, args map[string]any) (result any, rpcErr *rpcError, handled bool) {
	invalid := func(message string) (any, *rpcError, bool) {
		return nil, &rpcError{-32602, message}, true
	}
	switch name {
	case "script_list":
		kind, ok := optionalScriptType(args)
		if !ok {
			return invalid("type must be weapon, class or npc")
		}
		entries, err := s.backend.ListScripts(kind)
		if err != nil {
			result, rpcErr = toolError(err)
			return result, rpcErr, true
		}
		if query, _ := stringArg(args, "query"); strings.TrimSpace(query) != "" {
			query = strings.ToLower(strings.TrimSpace(query))
			filtered := make([]ScriptEntry, 0)
			for _, entry := range entries {
				if strings.Contains(strings.ToLower(entry.Name), query) {
					filtered = append(filtered, entry)
				}
			}
			entries = filtered
		}
		if entries == nil {
			entries = []ScriptEntry{}
		}
		result, rpcErr = textResult(map[string]any{"count": len(entries), "scripts": entries})
		return result, rpcErr, true
	case "script_read":
		ref, message := scriptRefArg(args)
		if message != "" {
			return invalid(message)
		}
		script, err := s.backend.ReadScript(ref)
		if err != nil {
			result, rpcErr = toolError(err)
			return result, rpcErr, true
		}
		result, rpcErr = textResult(script)
		return result, rpcErr, true
	case "script_write":
		ref, message := scriptRefArg(args)
		if message != "" {
			return invalid(message)
		}
		content, ok := stringArg(args, "script")
		if !ok {
			return invalid("script is required")
		}
		script, err := s.backend.WriteScript(ref, content)
		if err != nil {
			result, rpcErr = toolError(err)
			return result, rpcErr, true
		}
		result, rpcErr = textResult(writeOutcome(script, "updated"))
		return result, rpcErr, true
	case "weapon_add", "class_add", "npc_add":
		spec := NewScript{Type: strings.TrimSuffix(name, "_add")}
		var ok bool
		if spec.Name, ok = stringArg(args, "name"); !ok || strings.TrimSpace(spec.Name) == "" {
			return invalid("name is required")
		}
		spec.Name = strings.TrimSpace(spec.Name)
		spec.Script, _ = stringArg(args, "script")
		if spec.Type == "npc" {
			if _, present := args["id"]; present {
				id, valid := wholeArg(args, "id")
				if !valid {
					return invalid("id must be a non-negative integer")
				}
				spec.ID, spec.HasID = id, true
			}
			spec.NPCType, _ = stringArg(args, "npcType")
			spec.Scripter, _ = stringArg(args, "scripter")
			spec.Level, _ = stringArg(args, "level")
			if spec.X, ok = coordinateArg(args, "x"); !ok {
				return invalid("x must be a number")
			}
			if spec.Y, ok = coordinateArg(args, "y"); !ok {
				return invalid("y must be a number")
			}
		}
		script, err := s.backend.AddScript(spec)
		if err != nil {
			result, rpcErr = toolError(err)
			return result, rpcErr, true
		}
		result, rpcErr = textResult(writeOutcome(script, "created"))
		return result, rpcErr, true
	case "npc_reset", "npc_warp", "npc_flags_read", "npc_flags_write", "npc_attributes_read":
		ref, message := npcRefArg(args)
		if message != "" {
			return invalid(message)
		}
		switch name {
		case "npc_reset":
			result, rpcErr = toolDone(s.backend.ResetNPC(ref))
		case "npc_warp":
			level, ok := stringArg(args, "level")
			if !ok || strings.TrimSpace(level) == "" {
				return invalid("level is required")
			}
			x, okX := numberArg(args, "x")
			y, okY := numberArg(args, "y")
			if !okX || !okY {
				return invalid("x and y are required numbers")
			}
			result, rpcErr = toolDone(s.backend.WarpNPC(ref, strings.TrimSpace(level), x, y))
		case "npc_flags_read":
			flags, err := s.backend.ReadNPCFlags(ref)
			if err != nil {
				result, rpcErr = toolError(err)
			} else {
				result, rpcErr = textResult(map[string]any{"flags": flags})
			}
		case "npc_flags_write":
			flags, ok := stringArg(args, "flags")
			if !ok {
				return invalid("flags is required")
			}
			result, rpcErr = toolDone(s.backend.WriteNPCFlags(ref, flags))
		case "npc_attributes_read":
			attributes, err := s.backend.ReadNPCAttributes(ref)
			if err != nil {
				result, rpcErr = toolError(err)
			} else {
				result, rpcErr = textResult(map[string]any{"attributes": attributes})
			}
		}
		return result, rpcErr, true
	case "script_refresh":
		result, rpcErr = toolDone(s.backend.RefreshScripts())
		return result, rpcErr, true
	case "sync_status":
		result, rpcErr = textResult(s.backend.SyncStatus())
		return result, rpcErr, true
	}
	return nil, nil, false
}

// writeOutcome is the reply to a successful script write. It repeats what the
// server does next so the agent does not go looking for an "apply" step.
func writeOutcome(script Script, action string) map[string]any {
	outcome := map[string]any{
		"ok":     true,
		"action": action,
		"type":   script.Type,
		"name":   script.Name,
		"note":   "The server already applied this script. Use get_rc_chat to see the server's confirmation line or any compile error.",
	}
	if script.ID != nil {
		outcome["id"] = *script.ID
	}
	if script.LocalPath != "" {
		outcome["localPath"] = script.LocalPath
	}
	return outcome
}

func optionalScriptType(args map[string]any) (string, bool) {
	raw, present := args["type"]
	if !present {
		return "", true
	}
	kind, ok := raw.(string)
	if !ok {
		return "", false
	}
	kind = strings.ToLower(strings.TrimSpace(kind))
	return kind, kind == "weapon" || kind == "class" || kind == "npc"
}

// scriptRefArg reads {type, name, id}. The returned message is empty when the
// reference is usable.
func scriptRefArg(args map[string]any) (ScriptRef, string) {
	kind, ok := optionalScriptType(args)
	if !ok || kind == "" {
		return ScriptRef{}, "type must be weapon, class or npc"
	}
	if kind == "npc" {
		return npcRefArg(args)
	}
	name, _ := stringArg(args, "name")
	if strings.TrimSpace(name) == "" {
		return ScriptRef{}, "name is required"
	}
	return ScriptRef{Type: kind, Name: strings.TrimSpace(name)}, ""
}

func npcRefArg(args map[string]any) (ScriptRef, string) {
	ref := ScriptRef{Type: "npc"}
	if _, present := args["id"]; present {
		id, ok := wholeArg(args, "id")
		if !ok {
			return ScriptRef{}, "id must be a non-negative integer"
		}
		ref.ID, ref.HasID = id, true
		return ref, ""
	}
	name, _ := stringArg(args, "name")
	if strings.TrimSpace(name) == "" {
		return ScriptRef{}, "an NPC needs id or name"
	}
	ref.Name = strings.TrimSpace(name)
	return ref, ""
}

func numberArg(args map[string]any, name string) (float64, bool) {
	value, ok := args[name].(float64)
	if !ok || math.IsNaN(value) || math.IsInf(value, 0) {
		return 0, false
	}
	return value, true
}

func wholeArg(args map[string]any, name string) (int, bool) {
	value, ok := numberArg(args, name)
	if !ok || value < 0 || value > math.MaxInt32 || value != math.Trunc(value) {
		return 0, false
	}
	return int(value), true
}

// coordinateArg reads an optional NPC coordinate as the text the NC protocol
// expects. A missing value is "0".
func coordinateArg(args map[string]any, name string) (string, bool) {
	raw, present := args[name]
	if !present {
		return "0", true
	}
	switch value := raw.(type) {
	case float64:
		if math.IsNaN(value) || math.IsInf(value, 0) {
			return "", false
		}
		return strconv.FormatFloat(value, 'f', -1, 64), true
	case string:
		value = strings.TrimSpace(value)
		if _, err := strconv.ParseFloat(value, 64); err != nil {
			return "", false
		}
		return value, true
	}
	return "", false
}
