package mcp

import (
	"context"
	"encoding/json"
	"log"
	"time"
)

// callTool runs a tool and logs it, so that a dropped game session can be
// matched with what an agent asked for. Only the tool and its path are logged:
// scripts, file contents and chat messages stay out of the log file.
func (s *Server) callTool(ctx context.Context, raw json.RawMessage) (any, *rpcError) {
	var p struct {
		Name      string         `json:"name"`
		Arguments map[string]any `json:"arguments"`
	}
	if json.Unmarshal(raw, &p) != nil {
		return s.dispatchTool(ctx, raw)
	}
	path, _ := stringArg(p.Arguments, "path")
	log.Printf("[mcp] call tool=%s path=%q", p.Name, path)
	started := time.Now()
	result, rpcErr := s.dispatchTool(ctx, raw)
	log.Printf("[mcp] done tool=%s elapsed=%s error=%q", p.Name, time.Since(started).Round(time.Millisecond), toolFailure(result, rpcErr))
	return result, rpcErr
}

// toolFailure is the error a tool answered with; "" when it worked.
func toolFailure(result any, rpcErr *rpcError) string {
	if rpcErr != nil {
		return rpcErr.Message
	}
	value, _ := result.(map[string]any)
	if failed, _ := value["isError"].(bool); !failed {
		return ""
	}
	if content, _ := value["content"].([]map[string]string); len(content) > 0 {
		return content[0]["text"]
	}
	return "tool failed"
}
