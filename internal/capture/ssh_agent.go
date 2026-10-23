package capture

import (
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// sshAgentReady reports whether a socket is an ssh-agent that holds keys.
// Tests replace it.
var sshAgentReady = agentHasKeys

// ApplySSHAgent points ssh, scp, and rsync at an already-unlocked agent when
// this process was started without SSH_AUTH_SOCK. BatchMode will not prompt
// for a key passphrase, so the inherited agent is what makes the login work.
func ApplySSHAgent(cmd *exec.Cmd) {
	if cmd == nil || os.Getenv("SSH_AUTH_SOCK") != "" {
		return
	}
	sock := discoverSSHAuthSock()
	if sock == "" {
		return
	}
	cmd.Env = withSSHAuthSock(sock)
}

func withSSHAuthSock(sock string) []string {
	const prefix = "SSH_AUTH_SOCK="
	env := os.Environ()
	filtered := make([]string, 0, len(env)+1)
	for _, item := range env {
		if strings.HasPrefix(item, prefix) {
			continue
		}
		filtered = append(filtered, item)
	}
	return append(filtered, prefix+sock)
}

func discoverSSHAuthSock() string {
	var candidates []string
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		candidates = append(candidates, socketsIn(filepath.Join(home, ".ssh", "agent"))...)
	}
	if runtime := os.Getenv("XDG_RUNTIME_DIR"); runtime != "" {
		for _, name := range []string{
			"ssh-agent.socket",
			filepath.Join("keyring", "ssh"),
			filepath.Join("gnupg", "S.gpg-agent.ssh"),
		} {
			candidates = append(candidates, filepath.Join(runtime, name))
		}
	}
	for _, sock := range candidates {
		if isSocket(sock) && sshAgentReady(sock) {
			return sock
		}
	}
	return ""
}

func socketsIn(dir string) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var socks []string
	for _, entry := range entries {
		socks = append(socks, filepath.Join(dir, entry.Name()))
	}
	return socks
}

func isSocket(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeSocket != 0
}

func agentHasKeys(sock string) bool {
	sshAdd, err := exec.LookPath("ssh-add")
	if err != nil {
		return false
	}
	cmd := exec.Command(sshAdd, "-l")
	cmd.Env = withSSHAuthSock(sock)
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	return cmd.Run() == nil
}
