package lxcfreeze

import (
	"context"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/sudo"
)

// listerTools are the programs a Lister runs, in the order a missing one is named.
var listerTools = []string{"lxc-ls", "lxc-attach", "sudo"}

// Ordinary users of a container are those from this uid up to (not including) nobody's.
const (
	firstOrdinaryUID = 1000
	nobodyUID        = 65534
	passwdFields     = 7
	// guestPath is the PATH the play gives every command it runs in a container (members.py
	// GUEST_PATH): lxc-attach --clear-env leaves none.
	guestPath = "/usr/local/bin:/usr/bin:/bin:/usr/local/sbin:/usr/sbin:/sbin"
)

// userName is the grammar of a user the play accepts (members.py _USER_RE). A name read from
// inside a container that does not fit it is never shown, so a container cannot write to the
// terminal.
var userName = regexp.MustCompile(`^[a-z_][a-z0-9_-]{0,31}$`)

// Lister reads which containers the host has and who lives in them, for a tool that has to
// name one (ccy-teams adds a container's agent to a team). LXC here is rootful, so every
// question goes through Sudo; the caller makes it ready first, saying why root is wanted.
type Lister struct {
	Sudo     *sudo.Sudo
	LookPath func(string) (string, error)
}

// Containers are the host's containers by name, sorted: those running, and those that are not
// (stopped or frozen), which a member cannot be joined to.
func (l *Lister) Containers(ctx context.Context) ([]string, []string, error) {
	if err := l.preflight(); err != nil {
		return nil, nil, err
	}
	res, err := l.Sudo.Capture(ctx, "lxc-ls", "-1")
	all, err := namesOf(res, err)
	if err != nil {
		return nil, nil, err
	}
	res, err = l.Sudo.Capture(ctx, "lxc-ls", "-1", "--running")
	running, err := namesOf(res, err)
	if err != nil {
		return nil, nil, err
	}
	var stopped []string
	for _, name := range all {
		if !slices.Contains(running, name) {
			stopped = append(stopped, name)
		}
	}
	return running, stopped, nil
}

// Users are the ordinary login users of a running container: uid 1000 up to nobody's, with a
// shell that lets them log in. Only the passwd database is read; nothing in it is secret.
func (l *Lister) Users(ctx context.Context, container string) ([]string, error) {
	if err := l.preflight(); err != nil {
		return nil, err
	}
	res, err := l.Sudo.Capture(ctx, "lxc-attach", "-n", container, "--clear-env", "--", "env", "-i",
		"PATH="+guestPath, "HOME=/root", "getent", "passwd")
	if err != nil {
		return nil, fmt.Errorf("the users of %s could not be read: %s", container, failureWords(string(res.Stderr), err))
	}
	var users []string
	for line := range strings.SplitSeq(string(res.Stdout), "\n") {
		if name, ok := ordinaryUser(line); ok {
			users = append(users, name)
		}
	}
	return users, nil
}

// preflight refuses, saying so, when the LXC tools are not installed: that is not "no
// containers", nothing here could answer the question at all.
func (l *Lister) preflight() error {
	var missing []string
	for _, name := range listerTools {
		if _, err := l.LookPath(name); err != nil {
			missing = append(missing, name)
		}
	}
	if len(missing) == 0 {
		return nil
	}
	return fmt.Errorf("LXC is not installed on this host (missing: %s). "+
		"playbooks/imports/play-lxc-install-config.yml installs it; run that play and try again. "+
		"Do not install it by hand", strings.Join(missing, " "))
}

// namesOf is the sorted names an lxc-ls run printed, or why it failed.
func namesOf(res execx.Result, err error) ([]string, error) {
	if err != nil {
		return nil, fmt.Errorf("lxc-ls failed, so the container list is unknown: %s", failureWords(string(res.Stderr), err))
	}
	var names []string
	for line := range strings.SplitSeq(string(res.Stdout), "\n") {
		if name := strings.TrimSpace(line); name != "" {
			names = append(names, name)
		}
	}
	slices.Sort(names)
	return names, nil
}

// ordinaryUser is the name on a passwd line when it is a user a person logs in as.
func ordinaryUser(line string) (string, bool) {
	fields := strings.Split(line, ":")
	if len(fields) != passwdFields {
		return "", false
	}
	uid, err := strconv.Atoi(fields[2])
	if err != nil || uid < firstOrdinaryUID || uid >= nobodyUID {
		return "", false
	}
	if strings.HasSuffix(fields[6], "/nologin") || strings.HasSuffix(fields[6], "/false") || fields[6] == "" {
		return "", false
	}
	return fields[0], userName.MatchString(fields[0])
}

// failureWords is a command's own stderr, else the error that ended it.
func failureWords(stderr string, err error) string {
	if said := strings.TrimSpace(stderr); said != "" {
		return said
	}
	return err.Error()
}
