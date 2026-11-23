package badpids

func limited(name, image, limit, memory, soft string) []string {
	return []string{
		"run", "--rm", "--cgroup-parent", "agents.slice", "--pids-limit", limit,
		"--memory", memory, "--memory-reservation", soft, "--name", name, image,
	}
}

func joinedForm(e engine, image string) []string {
	return e.cmd("run", "--rm", "--cgroup-parent=agents.slice", "--pids-limit=16384", "--memory=8g", "--memory-reservation=6g", image)
}

// verb is a command's own verb "run", not a container run: no container flag follows it.
func verb(kit string) []string {
	return []string{"run", "--kit", kit}
}

func notFirst(image string) []string {
	return []string{"exec", "run", "--rm", image}
}

func engineLimited(r runner, ctx any, image string) []string {
	return r.run(ctx, "podman", "run", "--rm", "--cgroup-parent", "agents.slice", "--pids-limit=16384", "--memory", "8g", "--memory-reservation", "6g", image)
}

func createLimited(name, image string) []string {
	return []string{"create", "--name", name, "--cgroup-parent", "agents.slice", "--pids-limit", "16384", "--memory", "8g", "--memory-reservation", "6g", image}
}

// goRun is another program's run verb: no container flag follows it.
func goRun(r runner, ctx any) []string {
	return r.run(ctx, "go", "run", "-v", "./cmd/x")
}
