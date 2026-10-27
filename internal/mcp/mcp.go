// Package mcp serves the command catalog over stdio as an MCP tool server.
package mcp

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"backupbinder/internal/agent"
	"backupbinder/internal/fail"
)

// Call runs one tool. The map keys are flag names.
type Call func(name string, args map[string]any) (any, error)

type request struct {
	ID     *json.RawMessage `json:"id"`
	Method string           `json:"method"`
	Params json.RawMessage  `json:"params"`
}

// Serve reads newline-delimited or Content-Length JSON-RPC and writes newline-delimited responses.
func Serve(in io.Reader, out io.Writer, call Call) error {
	reader := bufio.NewReader(in)
	writer := bufio.NewWriter(out)
	for {
		body, err := readMessage(reader)
		if err != nil {
			if err == io.EOF {
				return nil
			}
			return err
		}
		var req request
		if err := json.Unmarshal(body, &req); err != nil {
			continue
		}
		if req.ID == nil {
			continue
		}
		result, rpcErr := handle(req, call)
		response := map[string]any{"jsonrpc": "2.0", "id": req.ID}
		if rpcErr != nil {
			response["error"] = rpcErr
		} else {
			response["result"] = result
		}
		encoded, err := json.Marshal(response)
		if err != nil {
			return err
		}
		if _, err := writer.Write(append(encoded, '\n')); err != nil {
			return err
		}
		if err := writer.Flush(); err != nil {
			return err
		}
	}
}

func handle(req request, call Call) (any, map[string]any) {
	switch req.Method {
	case "ping":
		return map[string]any{}, nil
	case "initialize":
		version := "2024-11-05"
		var params struct {
			ProtocolVersion string `json:"protocolVersion"`
		}
		_ = json.Unmarshal(req.Params, &params)
		if params.ProtocolVersion != "" {
			version = params.ProtocolVersion
		}
		return map[string]any{
			"protocolVersion": version,
			"capabilities":    map[string]any{"tools": map[string]any{}},
			"serverInfo":      map[string]any{"name": "backupbinder", "version": "0.1.0"},
			"instructions":    agent.CatalogDocument().Instructions,
		}, nil
	case "tools/list":
		return map[string]any{"tools": toolList()}, nil
	case "tools/call":
		var params struct {
			Name      string         `json:"name"`
			Arguments map[string]any `json:"arguments"`
		}
		if err := json.Unmarshal(req.Params, &params); err != nil {
			return nil, rpcError(-32602, err.Error())
		}
		if params.Arguments == nil {
			params.Arguments = map[string]any{}
		}
		value, err := call(params.Name, params.Arguments)
		text, isError := toolText(value, err)
		return map[string]any{
			"content": []map[string]any{{"type": "text", "text": text}},
			"isError": isError,
		}, nil
	default:
		return nil, rpcError(-32601, "method not found")
	}
}

func toolList() []map[string]any {
	var tools []map[string]any
	for _, cmd := range agent.CatalogDocument().Commands {
		if !cmd.Tool {
			continue
		}
		props := map[string]any{}
		var required []string
		for _, f := range cmd.Flags {
			schema := map[string]any{"type": "string", "description": f.Description}
			if f.Bool {
				schema["type"] = "boolean"
			} else if strings.Contains(f.Placeholder, "|") {
				schema["enum"] = strings.Split(f.Placeholder, "|")
			}
			props[f.Name] = schema
			if f.Required {
				required = append(required, f.Name)
			}
		}
		input := map[string]any{"type": "object", "properties": props}
		if len(required) > 0 {
			input["required"] = required
		}
		tools = append(tools, map[string]any{
			"name": cmd.Name, "description": cmd.Summary, "inputSchema": input,
		})
	}
	return tools
}

func toolText(value any, err error) (string, bool) {
	if err != nil {
		payload := map[string]any{"ok": false, "error": map[string]any{"code": "error", "message": err.Error()}}
		if fe, ok := err.(*fail.Error); ok {
			payload["error"] = fe
		}
		encoded, _ := json.MarshalIndent(payload, "", "  ")
		return string(encoded), true
	}
	encoded, _ := json.MarshalIndent(value, "", "  ")
	return string(encoded), false
}

func rpcError(code int, message string) map[string]any {
	return map[string]any{"code": code, "message": message}
}

func readMessage(reader *bufio.Reader) ([]byte, error) {
	line, err := reader.ReadBytes('\n')
	if err != nil {
		return nil, err
	}
	trimmed := bytes.TrimSpace(line)
	if len(trimmed) == 0 {
		return readMessage(reader)
	}
	if trimmed[0] == '{' {
		return trimmed, nil
	}
	header := string(trimmed)
	length := -1
	for {
		if strings.HasPrefix(strings.ToLower(header), "content-length:") {
			fmt.Sscanf(strings.TrimSpace(header[len("content-length:"):]), "%d", &length)
		}
		next, err := reader.ReadString('\n')
		if err != nil {
			return nil, err
		}
		if strings.TrimSpace(next) == "" {
			break
		}
		header = next
	}
	if length < 0 {
		return nil, io.ErrUnexpectedEOF
	}
	buf := make([]byte, length)
	if _, err := io.ReadFull(reader, buf); err != nil {
		return nil, err
	}
	return buf, nil
}
