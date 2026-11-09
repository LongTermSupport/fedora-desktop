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

type runner struct{}

func (runner) run(_ any, args ...string) []string { return args }

// enginePackage is the engine package's own call style: a context first, then the verb.
func enginePackage(r runner, ctx any, image string) []string {
	return r.run(ctx, "run", "--rm", image) // want `container run with the engine's default pids limit`
}

// programFirst names the engine before the verb, as an exec argv does.
func programFirst(r runner, ctx any, image string) []string {
	return r.run(ctx, "podman", "run", "--rm", image) // want `container run with the engine's default pids limit`
}

func dockerContainerRun(image string) []string {
	return []string{"docker", "container", "run", "-d", image} // want `container run with the engine's default pids limit`
}

// created is a container made to be started later: the limit is the create's to give.
func created(name, image string) []string {
	return []string{"create", "--name", name, image} // want `container run with the engine's default pids limit`
}

// named carries none of the attach or detach flags, only a container's name.
func named(name, image string) []string {
	return []string{"run", "--name", name, image} // want `container run with the engine's default pids limit`
}

func combinedShort(image string) []string {
	return []string{"run", "-dit", image} // want `container run with the engine's default pids limit`
}

func valued(image string) []string {
	return []string{"run", "--rm=true", "--detach=true", image} // want `container run with the engine's default pids limit`
}
