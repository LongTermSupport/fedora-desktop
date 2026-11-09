package nord

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

// The suffixes NordVPN's file names carry, stripped from a config's name (E5).
const ovpnExt = ".ovpn"

var nordSuffixes = []string{".nordvpn.com.udp", ".nordvpn.com.tcp"}

// config is one .ovpn file: its name, its file name less .ovpn (the long form, E6) and its
// path.
type config struct {
	name, long, path string
}

// shortName is a file name less .ovpn and a NordVPN suffix.
func shortName(long string) string {
	for _, s := range nordSuffixes {
		if cut, ok := strings.CutSuffix(long, s); ok && cut != "" {
			return cut
		}
	}
	return long
}

// configs are the .ovpn files in the configs directory, in byte order of their file names,
// each named as E5 says.
func (r *run) configs() ([]config, error) {
	dir := r.path(configsRel)
	entries, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, fmt.Errorf("the configs directory %s/ is missing; run %s, which creates it", r.abbrev(dir), play)
	}
	if err != nil {
		return nil, fmt.Errorf("cannot read the configs directory %s/: %w", r.abbrev(dir), err)
	}
	var out []config
	shorts := map[string]int{}
	for _, e := range entries {
		long, ok := strings.CutSuffix(e.Name(), ovpnExt)
		if !ok || long == "" {
			continue
		}
		path := filepath.Join(dir, e.Name())
		info, statErr := os.Stat(path)
		if statErr != nil || !info.Mode().IsRegular() {
			continue
		}
		out = append(out, config{name: shortName(long), long: long, path: path})
		shorts[shortName(long)]++
	}
	for i := range out {
		if shorts[out[i].name] > 1 {
			out[i].name = out[i].long
		}
	}
	return out, nil
}

// resolve is the config a name means (E6); false after saying why there is none.
func (r *run) resolve(name string) (config, bool) {
	all, err := r.configs()
	if err != nil {
		r.failf("%v.", err)
		return config{}, false
	}
	var shared []string
	for _, c := range all {
		if c.name == name || c.long == name {
			return c, true
		}
		if shortName(c.long) == name {
			shared = append(shared, c.long)
		}
	}
	if len(shared) > 0 {
		r.failf("'%s' names more than one config: %s; use one of those names.", name, strings.Join(shared, ", "))
		return config{}, false
	}
	r.failf("no config named '%s' in %s/.", name, r.abbrev(r.path(configsRel)))
	r.namesToStderr(all)
	return config{}, false
}

// listNamesToStderr is the names after a missing name (E2).
func (r *run) listNamesToStderr() {
	all, err := r.configs()
	if err != nil {
		r.failf("%v.", err)
		return
	}
	r.namesToStderr(all)
}

func (r *run) namesToStderr(all []config) {
	r.say("")
	if len(all) == 0 {
		r.noConfigsHint(r.say)
		return
	}
	r.say("Available configs:")
	for _, c := range all {
		r.say("  " + c.name)
	}
}

// noConfigsHint is where configs come from (E7), written by say.
func (r *run) noConfigsHint(say func(string)) {
	say("Download .ovpn files from NordVPN and place them in:")
	say("  " + r.abbrev(r.path(configsRel)) + "/")
	say("")
	say("Visit: nordvpn.com → Dashboard → Downloads → OpenVPN configs")
}

// list is E7.
func (r *run) list() term.ExitCode {
	all, err := r.configs()
	if err != nil {
		r.failf("%v.", err)
		return term.ExitFailure
	}
	r.print("Available VPN configs:")
	r.print("")
	for _, c := range all {
		r.print("  " + c.name)
	}
	r.print("")
	r.print(fmt.Sprintf("Total: %d configs", len(all)))
	r.print("")
	if len(all) == 0 {
		r.noConfigsHint(r.print)
		return term.ExitOK
	}
	r.print("To connect: nord connect <name>")
	r.print("Or run 'nord' for the interactive chooser.")
	return term.ExitOK
}
