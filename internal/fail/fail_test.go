package fail

import (
	"encoding/json"
	"errors"
	"testing"
)

func TestWrapAndJSON(t *testing.T) {
	if (*Error)(nil).Error() != "" {
		t.Fatal("nil error")
	}
	if Wrap("step_failed", "could not dump", nil).Error() != "could not dump" {
		t.Fatal(Wrap("step_failed", "could not dump", nil))
	}
	err := Wrap("step_failed", "could not dump", errors.New("exit 1"))
	if err.Error() != "could not dump: exit 1" || err.Code != "step_failed" {
		t.Fatal(err)
	}
	code := 2
	err.ExitCode = &code
	err.Step = "database_dump"
	raw, jsonErr := json.Marshal(err)
	if jsonErr != nil {
		t.Fatal(jsonErr)
	}
	var got map[string]any
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	if got["code"] != "step_failed" || got["step"] != "database_dump" || got["exit_code"] != float64(2) {
		t.Fatal(string(raw))
	}
	if _, ok := got["command"]; ok {
		t.Fatal(string(raw))
	}
}
