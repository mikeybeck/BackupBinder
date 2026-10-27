package app

import (
	"os"
	"os/exec"
	"path/filepath"

	"backupbinder/internal/destination"
)

// DoctorTool is one program the backup path may need.
type DoctorTool struct {
	Name   string `json:"name"`
	Found  bool   `json:"found"`
	Path   string `json:"path,omitempty"`
	Detail string `json:"detail,omitempty"`
}

// DoctorMCP is the stdio config an agent host can save.
type DoctorMCP struct {
	Command string   `json:"command"`
	Args    []string `json:"args"`
}

// DoctorReport is the machine check. It does not install anything.
type DoctorReport struct {
	OK                 bool         `json:"ok"`
	Home               string       `json:"home"`
	Ready              bool         `json:"ready"`
	Tools              []DoctorTool `json:"tools"`
	MCP                DoctorMCP    `json:"mcp"`
	ScheduleInvocation string       `json:"schedule_invocation"`
	Note               string       `json:"note"`
}

// Doctor reports the data home and helper programs. It runs when restic is missing.
func Doctor(home string) DoctorReport {
	if home == "" {
		home = DefaultHome()
	}
	if abs, err := filepath.Abs(home); err == nil {
		home = abs
	}
	exe := "backupbinder"
	if path, err := os.Executable(); err == nil && path != "" {
		exe = path
	}
	tools := []DoctorTool{
		toolStatus("restic", func() (string, error) { return FindRestic() }),
		toolStatus("rclone", func() (string, error) { return destination.FindTool("rclone") }),
		toolStatus("rsync", func() (string, error) { return exec.LookPath("rsync") }),
		toolStatus("ssh", func() (string, error) { return exec.LookPath("ssh") }),
	}
	ready := false
	for _, tool := range tools {
		if tool.Name == "restic" {
			ready = tool.Found
		}
	}
	return DoctorReport{
		OK: true, Home: home, Ready: ready, Tools: tools,
		MCP:                DoctorMCP{Command: exe, Args: []string{"--home", home, "mcp"}},
		ScheduleInvocation: "* * * * * " + exe + " --home " + home + " run_scheduled",
		Note:               "Nothing here is installed for you. The schedule line is not installed. mcp does not start until restic is found.",
	}
}

func toolStatus(name string, find func() (string, error)) DoctorTool {
	path, err := find()
	if err != nil || path == "" {
		detail := ""
		if err != nil {
			detail = err.Error()
		}
		return DoctorTool{Name: name, Detail: detail}
	}
	return DoctorTool{Name: name, Found: true, Path: path}
}
