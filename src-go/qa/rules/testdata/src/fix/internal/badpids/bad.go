// Package badpids starts containers with the engine's default pids limit.
package badpids

// session is ccc's session as it was: a long-running Claude Code container at podman's 2048
// threads (docker: none), which a fork bomb inside it climbs in seconds.
func session(name, image string) []string {
	return []string{"run", "--rm", "--interactive", "--tty", "--name", name, image} // want `container run with the engine's default pids limit`
}

type engine struct{}

func (engine) cmd(args ...string) []string { return args }

// probe is engine.ClaudeVersion as it was: a call whose arguments are the run.
func probe(e engine, image string) []string {
	return e.cmd("run", "--rm", "--entrypoint", "claude", image, "--version") // want `container run with the engine's default pids limit`
}

func detached(image string) []string {
	return []string{"run", "-d", image} // want `container run with the engine's default pids limit`
}
