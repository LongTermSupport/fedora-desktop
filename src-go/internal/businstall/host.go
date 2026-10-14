package businstall

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"strings"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/busadmin"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/firewalld"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/fsx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/hostnet"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/hostsys"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/systemd"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

// The directories the installer works in. Each is its own os.Root, opened below its parent,
// so a symlink that the agent-bus user plants in a directory it owns cannot lead a root write
// anywhere else; only the three top ones are opened by path.
const (
	usrLocalDir  = "/usr/local"
	unitsDir     = "/etc/systemd/system"
	varLibDir    = "/var/lib"
	stateDir     = "/var/lib/agent-bus"
	installDir   = "/var/lib/agent-bus-install"
	appliedDir   = "/var/lib/agent-bus-applied"
	libRel       = "lib/agent-bus"
	shareRel     = "share/agent-bus"
	kitRel       = "share/agent-bus/kit"
	hsTemplate   = "agent-bus-hs@.service"
	osReleaseRel = "/etc/os-release"

	dirMode        fs.FileMode = 0o755
	privateMode    fs.FileMode = 0o700
	programMode    fs.FileMode = 0o755
	fileMode       fs.FileMode = 0o644
	groupReadMode  fs.FileMode = 0o640
	privateFile    fs.FileMode = 0o600
	workDirPattern             = ".work-"
)

func (a *App) path(display string) string { return a.cfg.Prefix + display }

func hsUnit(team string) string     { return "agent-bus-hs@" + team + ".service" }
func timerUnit(team string) string  { return "agent-bus-backup@" + team + ".timer" }
func dropinName(team string) string { return "agent-bus-hs@" + team + ".service.d" }

// fsAt is the directory display, opened once per run. The three top directories are opened
// by path; every other one through its parent.
func (a *App) fsAt(display string) (*fsx.FS, error) {
	if f := a.dirs[display]; f != nil {
		return f, nil
	}
	var f *fsx.FS
	var err error
	switch display {
	case usrLocalDir, unitsDir, varLibDir:
		if display == varLibDir && a.cfg.Prefix != "" {
			// A test host has no /var/lib; a real one always does.
			if err = os.MkdirAll(a.path(display), 0o750); err != nil {
				return nil, fmt.Errorf("make %s: %w", a.path(display), err)
			}
		}
		f, err = fsx.Open(a.path(display), display, a.stdout, a.cfg.Pid)
	default:
		var parent *fsx.FS
		if parent, err = a.fsAt(path.Dir(display)); err != nil {
			return nil, err
		}
		f, err = parent.Sub(path.Base(display))
	}
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", display, err)
	}
	a.dirs[display] = f
	return f, nil
}

// tryFS is fsAt for a directory that may not exist: found is false then.
func (a *App) tryFS(display string) (*fsx.FS, bool, error) {
	f, err := a.fsAt(display)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	return f, true, nil
}

func (a *App) rootOwner() (fsx.Owner, error) {
	if a.root == nil {
		o, err := a.cfg.RootOwner()
		if err != nil {
			return fsx.Owner{}, fmt.Errorf("the root account: %w", err)
		}
		a.root = &o
	}
	return *a.root, nil
}

// svcOwner is the agent-bus account, looked up when needed: `software` creates it.
func (a *App) svcOwner() (fsx.Owner, error) {
	if a.svc == nil {
		o, err := a.cfg.SvcOwner()
		if err != nil {
			return fsx.Owner{}, fmt.Errorf("the %s account: %w", svcName, err)
		}
		a.svc = &o
	}
	return *a.svc, nil
}

// groupOwner is root's user with the service group: what the homeserver may read but not write.
func (a *App) groupOwner() (fsx.Owner, error) {
	root, err := a.rootOwner()
	if err != nil {
		return fsx.Owner{}, err
	}
	svc, err := a.svcOwner()
	if err != nil {
		return fsx.Owner{}, err
	}
	return fsx.Owner{UID: root.UID, GID: svc.GID}, nil
}

// ensureOSParent makes the directory name below f when the OS has not (a test prefix has
// none); one that exists is left exactly as it is.
func (a *App) ensureOSParent(f *fsx.FS, name string) error {
	if _, err := f.Lstat(name); err == nil {
		return nil
	} else if !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	root, err := a.rootOwner()
	if err != nil {
		return err
	}
	_, err = f.EnsureDir(name, dirMode, root)
	return err
}

// readRooted reads a file through its directory.
func readRooted(file string) ([]byte, error) {
	dir, err := os.OpenRoot(path.Dir(file))
	if err != nil {
		return nil, err
	}
	data, readErr := dir.ReadFile(path.Base(file))
	return data, errors.Join(readErr, dir.Close())
}

func (a *App) requireFedora() error {
	osRelease := a.path(osReleaseRel)
	data, err := readRooted(osRelease)
	if err != nil {
		return fail(term.ExitConfig, "%s is missing: only Fedora is supported", osRelease)
	}
	var ids []string
	for line := range strings.Lines(string(data)) {
		fields := strings.Split(strings.TrimRight(line, "\n"), "=")
		if len(fields) >= 2 && fields[0] == "ID" {
			ids = append(ids, strings.ReplaceAll(fields[1], `"`, ""))
		}
	}
	id := strings.Join(ids, "\n")
	if id != "fedora" {
		if id == "" {
			id = "<none>"
		}
		return fail(term.ExitConfig, "%s says ID=%s: only Fedora is supported", osRelease, id)
	}
	return nil
}

// newWorkDir makes root's private scratch directory beside the install records, on the
// filesystem the files end up on and out of reach of an inherited TMPDIR; finish removes it.
func (a *App) newWorkDir() (string, error) {
	varLib, err := a.fsAt(varLibDir)
	if err != nil {
		return "", err
	}
	root, err := a.rootOwner()
	if err != nil {
		return "", err
	}
	if _, err = varLib.EnsureDir(path.Base(installDir), privateMode, root); err != nil {
		return "", err
	}
	dir, err := os.MkdirTemp(a.path(installDir), workDirPattern)
	if err != nil {
		return "", fmt.Errorf("make a work directory: %w", err)
	}
	a.workDir = dir
	return dir, nil
}

// teamDoc is the part of a team's canonical team.json the installer reads.
type teamDoc struct {
	Team       string   `json:"team"`
	State      string   `json:"state"`
	Port       int      `json:"port"`
	ServerName string   `json:"server_name"`
	Listen     []string `json:"listen"`
	AllowFrom  []string `json:"allow_from"`
}

func parseTeamDoc(data []byte) (teamDoc, error) {
	var doc teamDoc
	if err := json.Unmarshal(data, &doc); err != nil {
		return teamDoc{}, err
	}
	return doc, nil
}

// decodeTeamDoc reads and decodes the team file name below f; a file that is not readable
// JSON is a configuration error naming the file.
func decodeTeamDoc(f *fsx.FS, name string) (teamDoc, error) {
	data, err := f.ReadFile(name)
	if err != nil {
		return teamDoc{}, failf("%v", err)
	}
	doc, err := parseTeamDoc(data)
	if err != nil {
		return teamDoc{}, fail(term.ExitConfig, "%s: not readable JSON: %v", f.Display(name), err)
	}
	return doc, nil
}

// readTeamDoc is decodeTeamDoc for a team of this host, whose file must also name a port.
func (a *App) readTeamDoc(f *fsx.FS, name string) (teamDoc, error) {
	doc, err := decodeTeamDoc(f, name)
	if err != nil {
		return teamDoc{}, err
	}
	if doc.Port == 0 {
		return teamDoc{}, fail(term.ExitConfig, "%s: has no port", f.Display(name))
	}
	return doc, nil
}

// requireTeamDir opens the team's directory in /var/lib/agent-bus; a team that is not
// installed here (no directory, or no team.json in it) is a configuration error.
func (a *App) requireTeamDir(team string) (*fsx.FS, error) {
	display := stateDir + "/" + team
	notHere := fail(term.ExitConfig, "team %s is not installed here (no %s/team.json)", team, display)
	teamFS, found, err := a.tryFS(display)
	if err != nil {
		return nil, failf("%v", err)
	}
	if !found {
		return nil, notHere
	}
	info, statErr := teamFS.Stat("team.json")
	if errors.Is(statErr, fs.ErrNotExist) || statErr == nil && !info.Mode().IsRegular() {
		return nil, notHere
	}
	if statErr != nil {
		return nil, failf("%v", statErr)
	}
	return teamFS, nil
}

// The adapters over the host's programs, all sending their chatter to stderr.

func (a *App) sd() *systemd.Systemd { return &systemd.Systemd{Runner: a.cfg.Runner, Chatter: a.stderr} }

func (a *App) net() *hostnet.Net { return &hostnet.Net{Runner: a.cfg.Runner, Chatter: a.stderr} }

func (a *App) sys() *hostsys.Sys { return &hostsys.Sys{Runner: a.cfg.Runner, Chatter: a.stderr} }

func (a *App) fw() *firewalld.Firewall {
	return &firewalld.Firewall{Runner: a.cfg.Runner, Chatter: a.stderr, Out: a.stdout}
}

func (a *App) admin() *busadmin.Admin {
	return &busadmin.Admin{Runner: a.cfg.Runner, Tool: a.cfg.AdminTool, Stdout: a.stdout, Stderr: a.stderr}
}

func (a *App) bundler() *busadmin.Bundler {
	return &busadmin.Bundler{Runner: a.cfg.Runner, Stderr: a.stderr}
}
