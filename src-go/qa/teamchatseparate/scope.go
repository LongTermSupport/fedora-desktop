package teamchatseparate

import "strings"

// v2Prefix is how every v2 name begins (D 3.1).
const v2Prefix = "teamchat"

// v2Packages holds every v2 Go package.
const v2Packages = "src-go/internal/teamchat/"

// homes are the two named homes of deliberate v1 references inside v2Packages (D 4.1): the v1
// constants v2 needs to stay away from v1, and the migration's v1 adapter.
var homes = []string{
	v2Packages + "v1names/",
	v2Packages + "migrate/v1/",
}

// ScanPaths are the paths the git listing is asked for: every scoped file lies below one.
func ScanPaths() []string {
	return []string{"src-go/internal/teamchat", "src-go/cmd", "files", "playbooks/imports", "tasks"}
}

// InScope is whether v2 owns the file (D 4.1 part 1): the v2 Go packages bar the two homes, a
// command under src-go/cmd/teamchat*, anything under files/ or tasks/ below a component named
// teamchat*, and a play playbooks/imports/play-teamchat*.yml.
func InScope(p string) bool {
	switch {
	case strings.HasPrefix(p, v2Packages):
		for _, home := range homes {
			if strings.HasPrefix(p, home) {
				return false
			}
		}
		return true
	case strings.HasPrefix(p, "src-go/cmd/"):
		return strings.HasPrefix(strings.TrimPrefix(p, "src-go/cmd/"), v2Prefix)
	case strings.HasPrefix(p, "files/"), strings.HasPrefix(p, "tasks/"):
		parts := strings.Split(p, "/")
		for _, part := range parts[1:] {
			if strings.HasPrefix(part, v2Prefix) {
				return true
			}
		}
		return false
	case strings.HasPrefix(p, "playbooks/imports/"):
		name := strings.TrimPrefix(p, "playbooks/imports/")
		return !strings.Contains(name, "/") && strings.HasPrefix(name, "play-"+v2Prefix) && strings.HasSuffix(name, ".yml")
	}
	return false
}
