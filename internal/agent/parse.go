package agent

import (
	"flag"
	"fmt"
)

// Parse reads CLI flags for a catalog command into a map keyed by flag name.
func Parse(name string, argv []string) (map[string]any, error) {
	cmd, ok := Lookup(name)
	if !ok {
		return nil, fmt.Errorf("unknown command %s", name)
	}
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	strings := map[string]*string{}
	bools := map[string]*bool{}
	for _, f := range cmd.Flags {
		if f.Bool {
			bools[f.Name] = fs.Bool(f.Name, false, f.Description)
			continue
		}
		strings[f.Name] = fs.String(f.Name, "", f.Description)
	}
	if err := fs.Parse(argv); err != nil {
		return nil, err
	}
	visited := map[string]bool{}
	fs.Visit(func(f *flag.Flag) { visited[f.Name] = true })
	out := map[string]any{}
	for key, value := range strings {
		if visited[key] {
			out[key] = *value
		}
	}
	for key, value := range bools {
		if visited[key] {
			out[key] = *value
		}
	}
	return out, nil
}

func asString(args map[string]any, key string) string {
	switch value := args[key].(type) {
	case string:
		return value
	case nil:
		return ""
	default:
		return fmt.Sprint(value)
	}
}

func asBool(args map[string]any, key string) bool {
	switch value := args[key].(type) {
	case bool:
		return value
	case string:
		return value == "true" || value == "1"
	default:
		return false
	}
}

func asBoolPtr(args map[string]any, key string) *bool {
	if _, ok := args[key]; !ok {
		return nil
	}
	value := asBool(args, key)
	return &value
}

func argSet(args map[string]any, key string) bool {
	_, ok := args[key]
	return ok
}
