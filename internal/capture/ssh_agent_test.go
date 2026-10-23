package capture

import (
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestApplySSHAgentUsesUnlockedSocket(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("SSH_AUTH_SOCK", "")
	t.Setenv("XDG_RUNTIME_DIR", "")
	dir := filepath.Join(home, ".ssh", "agent")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	sock := filepath.Join(dir, "s.test.agent")
	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })

	previous := sshAgentReady
	sshAgentReady = func(got string) bool { return got == sock }
	t.Cleanup(func() { sshAgentReady = previous })

	cmd := exec.Command("rsync")
	ApplySSHAgent(cmd)
	if !envHas(cmd.Env, "SSH_AUTH_SOCK="+sock) {
		t.Fatalf("env %#v", cmd.Env)
	}
	if envHasPrefixCount(cmd.Env, "SSH_AUTH_SOCK=") != 1 {
		t.Fatalf("env %#v", cmd.Env)
	}
}

func TestApplySSHAgentKeepsInheritedSocket(t *testing.T) {
	t.Setenv("SSH_AUTH_SOCK", "/tmp/already-set.sock")
	cmd := exec.Command("rsync")
	ApplySSHAgent(cmd)
	if cmd.Env != nil {
		t.Fatalf("env %#v", cmd.Env)
	}
}

func TestApplySSHAgentIgnoresDeadSocket(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("SSH_AUTH_SOCK", "")
	t.Setenv("XDG_RUNTIME_DIR", "")
	dir := filepath.Join(home, ".ssh", "agent")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	sock := filepath.Join(dir, "s.dead.agent")
	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })

	previous := sshAgentReady
	sshAgentReady = func(string) bool { return false }
	t.Cleanup(func() { sshAgentReady = previous })

	cmd := exec.Command("rsync")
	ApplySSHAgent(cmd)
	if cmd.Env != nil {
		t.Fatalf("env %#v", cmd.Env)
	}
}

func envHas(env []string, want string) bool {
	for _, item := range env {
		if item == want {
			return true
		}
	}
	return false
}

func envHasPrefixCount(env []string, prefix string) int {
	n := 0
	for _, item := range env {
		if strings.HasPrefix(item, prefix) {
			n++
		}
	}
	return n
}
