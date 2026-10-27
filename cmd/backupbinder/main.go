package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	"backupbinder/internal/agent"
	"backupbinder/internal/app"
	"backupbinder/internal/fail"
	"backupbinder/internal/mcp"
	"backupbinder/internal/ui"
)

func main() {
	home, args := splitHome(os.Args[1:])
	if len(args) == 0 {
		usage()
		os.Exit(2)
	}
	code := run(home, args[0], args[1:])
	os.Exit(code)
}

func splitHome(args []string) (string, []string) {
	var home string
	var rest []string
	for i := 0; i < len(args); i++ {
		switch {
		case args[i] == "--home" && i+1 < len(args):
			home = args[i+1]
			i++
		case strings.HasPrefix(args[i], "--home="):
			home = strings.TrimPrefix(args[i], "--home=")
		default:
			rest = append(rest, args[i])
		}
	}
	return home, rest
}

func run(home, command string, args []string) int {
	if _, ok := agent.Lookup(command); !ok {
		usage()
		return 2
	}
	if command == "agent" || command == "doctor" {
		return write(agent.Execute(nil, command, map[string]any{}))
	}
	if command == "discover_site" {
		parsed, err := agent.Parse(command, args)
		if err != nil {
			return writeErr(err)
		}
		return write(agent.Execute(nil, command, parsed))
	}
	application, err := app.Open(home)
	if err != nil {
		return writeErr(err)
	}
	defer application.Close()
	switch command {
	case "mcp":
		err := mcp.Serve(os.Stdin, os.Stdout, func(name string, callArgs map[string]any) (any, error) {
			return agent.Execute(application, name, callArgs)
		})
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		return 0
	case "serve":
		return serve(application, args)
	default:
		parsed, err := agent.Parse(command, args)
		if err != nil {
			return writeErr(err)
		}
		return write(agent.Execute(application, command, parsed))
	}
}

func serve(application *app.App, args []string) int {
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	host := fs.String("host", "127.0.0.1", "loopback address")
	port := fs.Int("port", 47831, "port")
	if err := fs.Parse(args); err != nil {
		return writeErr(err)
	}
	if *host != "127.0.0.1" && *host != "localhost" && *host != "::1" {
		return writeErr(fail.New("bad_address", "The UI binds to localhost only."))
	}
	addr := net.JoinHostPort(*host, fmt.Sprintf("%d", *port))
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return writeErr(fail.Wrap("bad_address", "could not listen", err))
	}
	url := "http://" + addr + "/"
	write(map[string]any{"ok": true, "url": url}, nil)
	server := &http.Server{Handler: ui.Handler(application), ReadHeaderTimeout: 10 * time.Second}
	if err := server.Serve(listener); err != nil && err != http.ErrServerClosed {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	return 0
}

func write(value any, err error) int {
	if err != nil {
		return writeErr(err)
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(value); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	return 0
}

func writeErr(err error) int {
	payload := map[string]any{"ok": false, "error": map[string]any{"code": "error", "message": err.Error()}}
	if fe, ok := err.(*fail.Error); ok {
		payload["error"] = fe
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	_ = enc.Encode(payload)
	return 1
}

func usage() {
	fmt.Fprint(os.Stderr, agent.Usage())
}
