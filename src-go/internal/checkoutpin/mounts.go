package checkoutpin

import (
	"slices"
	"strings"
)

// Mount is one validated line of a project's mounts file: the host path, the container path
// and ro or rw.
type Mount struct {
	Line          int
	Src, Dst, Opt string
}

// Problem is one rule a line of the mounts file breaks.
type Problem struct {
	Line int
	Why  string
}

// hostDenied are the host paths no mounts line may bind, nor any path under one, nor any
// directory above one: credentials, the launcher's own state (the session registry and this
// package's approvals), what the host runs as the user (programs, shell start-up files, user
// units, autostart entries, the container engine's configuration and storage), and the system
// roots. ~ is the home directory.
var hostDenied = []string{
	"~/.ssh", "~/.gnupg", "~/.aws", "~/.kube", "~/.claude", "~/.claude.json", "~/.claude-tokens",
	"~/.config/gh", "~/.local/share/keyrings", "~/.password-store",
	"~/.local/bin", "~/bin", "~/.local/state", "~/.cache/ccy",
	"~/.bashrc", "~/.bashrc.d", "~/.bash_profile", "~/.bash_login", "~/.bash_logout", "~/.profile",
	"~/.zshrc", "~/.zprofile", "~/.gitconfig", "~/.config/git",
	"~/.config/systemd", "~/.local/share/systemd", "~/.config/autostart", "~/.config/environment.d",
	"~/.config/containers", "~/.local/share/containers", "~/.local/share/applications",
	"~/.local/share/gnome-shell", "~/.tmux.conf", "~/.config/tmux",
	"/etc", "/root", "/run", "/var/run", "/var/local", "/proc", "/sys", "/dev", "/boot",
}

// containerDenied are the container paths no line may bind over: the workspace itself and its
// state, and what the image needs to run. `/` and `/workspace` are refused exactly, the rest with
// everything under them.
var containerDenied = []string{
	"/", "/workspace", "/workspace/.claude", "/root", "/home", "/etc", "/usr", "/bin", "/sbin", "/lib",
	"/lib64", "/opt", "/proc", "/sys", "/dev", "/run", "/var", "/tmp/claude-config-import",
}

// ParseMounts validates a mounts file (Plan 00266 Task A.2). Each line is
// <host-src>:<container-dst>[:ro|rw]; `#` starts a comment and blank lines are skipped. `~/` or
// `$HOME/` at the start of the host path is the home directory, and nothing else is expanded.
// Both paths are absolute and free of `..`, of `,` and of control characters. The host path is
// not the root or the home directory, not the project or a directory above it, and neither
// under, nor above, a denied path (hostDenied, with extra: the launcher's state when it lives
// elsewhere). The container path is not one of containerDenied, nor two lines' alike. A line
// that breaks a rule gives a Problem for each and no Mount. What is on disk is the caller's to
// check (Resolve).
func ParseMounts(content []byte, home, project string, extra []string) ([]Mount, []Problem) {
	denied := Denied(home, extra)
	var mounts []Mount
	var problems []Problem
	seen := map[string]bool{}
	for i, raw := range strings.Split(string(content), "\n") {
		line := strings.TrimSpace(strings.SplitN(raw, "#", 2)[0])
		if line == "" {
			continue
		}
		m, whys := parseLine(line, home, project, denied)
		if len(whys) == 0 && seen[m.Dst] {
			whys = append(whys, "container path declared twice: "+m.Dst)
		}
		if len(whys) > 0 {
			for _, why := range whys {
				problems = append(problems, Problem{Line: i + 1, Why: why})
			}
			continue
		}
		seen[m.Dst] = true
		m.Line = i + 1
		mounts = append(mounts, m)
	}
	return mounts, problems
}

// Denied is every host path no mounts line may bind, nor reach through a link: hostDenied in
// home, and extra.
func Denied(home string, extra []string) []string {
	denied := slices.Clone(extra)
	for _, d := range hostDenied {
		denied = append(denied, expandHome(d, home))
	}
	return denied
}

func parseLine(line, home, project string, denied []string) (Mount, []string) {
	parts := strings.Split(line, ":")
	if len(parts) < 2 || len(parts) > 3 {
		return Mount{}, []string{"expected <host-src>:<container-dst>[:ro|rw], got: " + line}
	}
	m := Mount{Src: trimSlash(expandHome(parts[0], home)), Dst: trimSlash(parts[1]), Opt: "rw"}
	if len(parts) == 3 && parts[2] != "" {
		m.Opt = parts[2]
	}
	var whys []string
	for _, p := range []string{m.Src, m.Dst} {
		whys = append(whys, pathProblems(p)...)
	}
	if m.Opt != "ro" && m.Opt != "rw" {
		whys = append(whys, "option must be ro or rw, got: "+m.Opt)
	}
	whys = append(whys, HostProblems(m.Src, home, project, denied)...)
	for _, d := range containerDenied {
		exact := d == "/" || d == "/workspace"
		if m.Dst == d || (!exact && under(m.Dst, d)) {
			whys = append(whys, "container path is off limits: "+m.Dst+" (under "+d+")")
			break
		}
	}
	return m, whys
}

// pathProblems is what is wrong with one path as either side of a bind.
func pathProblems(p string) []string {
	var whys []string
	if !strings.HasPrefix(p, "/") {
		whys = append(whys, "not an absolute path: "+p)
	}
	if slices.Contains(strings.Split(p, "/"), "..") {
		whys = append(whys, "'..' is not allowed: "+p)
	}
	if strings.ContainsFunc(p, func(r rune) bool { return r == ',' || r < 0x20 || r == 0x7f }) {
		whys = append(whys, "a comma or a control character cannot be bound: "+p)
	}
	return whys
}

// HostProblems is why a host path may not be bound: the root, the home directory, the project or
// a directory above it, or a path under or above one of denied (Denied's). The caller asks it again of the
// path the source resolves to, so a link cannot reach a denied path.
func HostProblems(src, home, project string, denied []string) []string {
	if src == "/" || src == home {
		return []string{"host path is off limits: " + src}
	}
	if src == project || under(project, src) {
		return []string{"host path is the project or a directory above it: " + src}
	}
	for _, d := range denied {
		if src == d || under(src, d) {
			return []string{"host path is off limits: " + src + " (under " + d + ")"}
		}
		if under(d, src) {
			return []string{"host path is off limits: " + src + " (it holds " + d + ")"}
		}
	}
	return nil
}

// under is whether path is strictly below dir.
func under(path, dir string) bool {
	return strings.HasPrefix(path, strings.TrimSuffix(dir, "/")+"/")
}

func expandHome(p, home string) string {
	for _, prefix := range []string{"~/", "$HOME/"} {
		if rest, ok := strings.CutPrefix(p, prefix); ok {
			return home + "/" + rest
		}
	}
	return p
}

func trimSlash(p string) string {
	if p == "/" {
		return p
	}
	return strings.TrimSuffix(p, "/")
}
