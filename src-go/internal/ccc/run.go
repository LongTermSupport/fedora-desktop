package ccc

import (
	"strconv"
	"strings"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/ccclaude"
)

const (
	// image is ccy's image; ccc builds none of its own.
	image = "claude-yolo:latest"
	// imageVersionLabel is the image's version, which REQUIRED_CONTAINER_VERSION is held to.
	imageVersionLabel = "claude-yolo-version"
	// containerWorkspace is the folder's path in the container.
	containerWorkspace = "/workspace"
	// containerState is the agent's state directory in the container.
	containerState = "/var/lib/ccc"
	// podmanProgram runs every container; ccc is rootless podman only.
	podmanProgram = "podman"
	// podmanNetwork is the network ccy's containers join by default.
	podmanNetwork = "podman"
)

// session is one container run's inputs.
type session struct {
	Agent    Agent
	Folder   string
	State    string
	Name     string
	TimeZone string
	Term     string
	Continue bool
}

// networkArgs is the container's network: the network ccy uses by default, never the host's,
// and no published port. It is the one place the per-container network policy (issue #114,
// Plan 00232) is applied when it lands.
func networkArgs() []string {
	return []string{"--network", podmanNetwork}
}

// sessionArgs is the podman argv of a session: only the folder and the agent's state mounted,
// the token passed by name, and the agent started by tini, not by ccy's entrypoint (which
// needs GitHub).
func sessionArgs(s session) []string {
	termName := s.Term
	if termName == "" {
		termName = "xterm"
	}
	args := []string{
		"run", "--rm", "--interactive", "--tty",
		"--name", s.Name,
		"--label", "ccc=true", "--label", "ccc-agent=" + s.Agent.Name,
	}
	args = append(args, networkArgs()...)
	args = append(args,
		"--entrypoint", "/usr/bin/tini",
		"--volume", s.Folder+":"+containerWorkspace+":z",
		"--volume", s.State+":"+containerState+":z",
		"--workdir", containerWorkspace,
		"--env", s.Agent.LoginEnv,
	)
	if s.TimeZone != "" {
		args = append(args, "--env", "TZ="+s.TimeZone)
	}
	args = append(args,
		"--env", "TERM="+termName,
		"--env", "COLORTERM=truecolor",
		"--env", s.Agent.ConfigEnv+"="+containerState,
		"--env", "IS_SANDBOX=1",
		"--env", ccclaude.WindowVar+"="+ccclaude.DefaultWindow,
		image, "--",
	)
	return append(args, s.Agent.Command(s.Continue)...)
}

// signInArgs is the podman argv of a sign-in: a throwaway container with nothing mounted.
func signInArgs(a Agent) []string {
	args := []string{"run", "--rm", "--interactive", "--tty"}
	args = append(args, networkArgs()...)
	args = append(args, "--entrypoint", a.Entrypoint, image)
	return append(args, a.SignIn...)
}

// versionAtLeast reports whether the dotted version have is want or later; anything that is
// not three numbers is not.
func versionAtLeast(have, want string) bool {
	h, okH := parseVersion(have)
	w, okW := parseVersion(want)
	if !okH || !okW {
		return false
	}
	for i := range h {
		if h[i] != w[i] {
			return h[i] > w[i]
		}
	}
	return true
}

func parseVersion(v string) ([3]int, bool) {
	var out [3]int
	parts := strings.Split(v, ".")
	if len(parts) != len(out) {
		return out, false
	}
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 {
			return out, false
		}
		out[i] = n
	}
	return out, true
}
