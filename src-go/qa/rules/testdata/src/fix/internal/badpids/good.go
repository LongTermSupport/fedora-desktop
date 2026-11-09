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
