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
	out := map[string]any{}
	for key, value := range strings {
		out[key] = *value
	}
	for key, value := range bools {
		out[key] = *value
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
