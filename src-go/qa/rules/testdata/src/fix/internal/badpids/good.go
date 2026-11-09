package badpids

func limited(name, image, limit string) []string {
	return []string{"run", "--rm", "--pids-limit", limit, "--name", name, image}
}

func joinedForm(e engine, image string) []string {
	return e.cmd("run", "--rm", "--pids-limit=4096", image)
}

// verb is a command's own verb "run", not a container run: no container flag follows it.
func verb(kit string) []string {
	return []string{"run", "--kit", kit}
}

func notFirst(image string) []string {
	return []string{"exec", "run", "--rm", image}
}

func engineLimited(r runner, ctx any, image string) []string {
	return r.run(ctx, "podman", "run", "--rm", "--pids-limit=4096", image)
}

func createLimited(name, image string) []string {
	return []string{"create", "--name", name, "--pids-limit", "4096", image}
}

// goRun is another program's run verb: no container flag follows it.
func goRun(r runner, ctx any) []string {
	return r.run(ctx, "go", "run", "-v", "./cmd/x")
}
