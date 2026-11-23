// Package badpids starts containers without ccy's whole limit set.
package badpids

// session is ccc's session as it was: a long-running Claude Code container at podman's 2048
// threads (docker: none), which a fork bomb inside it climbs in seconds.
func session(name, image string) []string {
	return []string{"run", "--rm", "--interactive", "--tty", "--name", name, image} // want `container run without ccy's limit set`
}

type engine struct{}

func (engine) cmd(args ...string) []string { return args }

// probe is engine.ClaudeVersion as it was: a call whose arguments are the run.
func probe(e engine, image string) []string {
	return e.cmd("run", "--rm", "--entrypoint", "claude", image, "--version") // want `container run without ccy's limit set`
}

func detached(image string) []string {
	return []string{"run", "-d", image} // want `container run without ccy's limit set`
}

type runner struct{}

func (runner) run(_ any, args ...string) []string { return args }

// enginePackage is the engine package's own call style: a context first, then the verb.
func enginePackage(r runner, ctx any, image string) []string {
	return r.run(ctx, "run", "--rm", image) // want `container run without ccy's limit set`
}

// programFirst names the engine before the verb, as an exec argv does.
func programFirst(r runner, ctx any, image string) []string {
	return r.run(ctx, "podman", "run", "--rm", image) // want `container run without ccy's limit set`
}

func dockerContainerRun(image string) []string {
	return []string{"docker", "container", "run", "-d", image} // want `container run without ccy's limit set`
}

// created is a container made to be started later: the limit is the create's to give.
func created(name, image string) []string {
	return []string{"create", "--name", name, image} // want `container run without ccy's limit set`
}

// named carries none of the attach or detach flags, only a container's name.
func named(name, image string) []string {
	return []string{"run", "--name", name, image} // want `container run without ccy's limit set`
}

func combinedShort(image string) []string {
	return []string{"run", "-dit", image} // want `container run without ccy's limit set`
}

func valued(image string) []string {
	return []string{"run", "--rm=true", "--detach=true", image} // want `container run without ccy's limit set`
}

// pidsOnly is the limit set as it was before the memory caps: a pids limit, but no slice and
// no memory cap, so one container could take the host's memory.
func pidsOnly(name, image, limit string) []string {
	return []string{"run", "--rm", "--pids-limit", limit, "--name", name, image} // want `container run without ccy's limit set: missing --cgroup-parent, --memory, --memory-reservation`
}

// noSoftLimit has the hard cap but nothing that throttles before it.
func noSoftLimit(e engine, image string) []string {
	return e.cmd("run", "--rm", "--cgroup-parent=agents.slice", "--pids-limit=16384", "--memory=8g", image) // want `missing --memory-reservation`
}

// noSlice caps the container but not all of them together.
func noSlice(name, image string) []string {
	return []string{"create", "--name", name, "--pids-limit", "16384", "--memory", "8g", "--memory-reservation", "6g", image} // want `missing --cgroup-parent`
}
