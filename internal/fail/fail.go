// Package fail is the JSON error shape for the CLI and UI.
package fail

import "fmt"

// Error is returned as {"ok": false, "error": {...}}.
type Error struct {
	Code     string   `json:"code"`
	Message  string   `json:"message"`
	Step     string   `json:"step,omitempty"`
	Command  []string `json:"command,omitempty"`
	ExitCode *int     `json:"exit_code,omitempty"`
	Stderr   string   `json:"stderr,omitempty"`
	Hint     string   `json:"hint,omitempty"`
	RunID    string   `json:"run_id,omitempty"`
}

func (e *Error) Error() string {
	if e == nil {
		return ""
	}
	return e.Message
}

func New(code, message string) *Error {
	return &Error{Code: code, Message: message}
}

func Wrap(code, message string, err error) *Error {
	if err == nil {
		return New(code, message)
	}
	return New(code, fmt.Sprintf("%s: %s", message, err.Error()))
}

func ExitCode(code int) *int {
	return &code
}
