package mcp

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"local-backup/internal/agent"
)

func TestServeListsSetupAndCallsAgent(t *testing.T) {
	var in bytes.Buffer
	write := func(method string, id int, params any) {
		body, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": id, "method": method, "params": params})
		in.Write(append(body, '\n'))
	}
	write("initialize", 1, map[string]any{"protocolVersion": "2024-11-05"})
	write("tools/list", 2, map[string]any{})
	write("tools/call", 3, map[string]any{"name": "agent", "arguments": map[string]any{}})
	var out bytes.Buffer
	if err := Serve(&in, &out, func(name string, args map[string]any) (any, error) {
		return agent.Execute(nil, name, args)
	}); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) != 3 {
		t.Fatalf("lines %d\n%s", len(lines), out.String())
	}
	var listed struct {
		Result struct {
			Tools []struct {
				Name string `json:"name"`
			} `json:"tools"`
		} `json:"result"`
	}
	if err := json.Unmarshal([]byte(lines[1]), &listed); err != nil {
		t.Fatal(err)
	}
	sawSetup := false
	for _, tool := range listed.Result.Tools {
		if tool.Name == "setup" {
			sawSetup = true
		}
		if tool.Name == "mcp" || tool.Name == "serve" {
			t.Fatalf("tool %s", tool.Name)
		}
	}
	if !sawSetup {
		t.Fatal("setup missing")
	}
	var called struct {
		Result struct {
			IsError bool `json:"isError"`
			Content []struct {
				Text string `json:"text"`
			} `json:"content"`
		} `json:"result"`
	}
	if err := json.Unmarshal([]byte(lines[2]), &called); err != nil {
		t.Fatal(err)
	}
	if called.Result.IsError || !strings.Contains(called.Result.Content[0].Text, "ask the user") {
		t.Fatal(called.Result.Content[0].Text)
	}
}

func TestServeContentLengthAndToolError(t *testing.T) {
	body := []byte(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"nope","arguments":{}}}`)
	var in bytes.Buffer
	fmt.Fprintf(&in, "Content-Length: %d\r\n\r\n", len(body))
	in.Write(body)
	in.WriteString(`{"jsonrpc":"2.0","method":"notifications/initialized"}` + "\n")
	in.WriteString(`{"jsonrpc":"2.0","id":2,"method":"unknown"}` + "\n")

	var out bytes.Buffer
	if err := Serve(&in, &out, func(name string, args map[string]any) (any, error) {
		return agent.Execute(nil, name, args)
	}); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) != 2 {
		t.Fatalf("lines %d\n%s", len(lines), out.String())
	}
	var tool struct {
		Result struct {
			IsError bool `json:"isError"`
			Content []struct {
				Text string `json:"text"`
			} `json:"content"`
		} `json:"result"`
	}
	if err := json.Unmarshal([]byte(lines[0]), &tool); err != nil {
		t.Fatal(err)
	}
	if !tool.Result.IsError || !strings.Contains(tool.Result.Content[0].Text, "bad_command") {
		t.Fatal(tool.Result.Content[0].Text)
	}
	var missing struct {
		Error struct {
			Code int `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal([]byte(lines[1]), &missing); err != nil {
		t.Fatal(err)
	}
	if missing.Error.Code != -32601 {
		t.Fatalf("%s", lines[1])
	}
}
