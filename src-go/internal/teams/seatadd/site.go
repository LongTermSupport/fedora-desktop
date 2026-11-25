package seatadd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/declare"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/declare/hostvars"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/system"
)

// HostVarsSite is the Site seam over the host_vars file at path: its teamchat_site, "" when it
// declares none. The file is read whole by the strict v2 reader, which reads only the v2 keys,
// so a malformed v2 declaration refuses the run rather than issue under a guessed site.
func HostVarsSite(path string) func() (string, error) {
	return func() (string, error) {
		d, err := readHostVars(path)
		return d.Site, err
	}
}

// HostVarsTeams is system.Life's Declared over the host_vars file at path: what its
// teamchat_teams say of a team (register R59), read on each call by the same strict reader, so a
// malformed declaration refuses rather than reads as no entry.
func HostVarsTeams(path string) func(paths.TeamID) (system.Declared, error) {
	return func(team paths.TeamID) (system.Declared, error) {
		d, err := readHostVars(path)
		if err != nil {
			return system.NotDeclared, err
		}
		for _, t := range d.Teams {
			if t.ID != team {
				continue
			}
			if t.State == declare.StateAbsent {
				return system.DeclaredAbsent, nil
			}
			return system.DeclaredPresent, nil
		}
		return system.NotDeclared, nil
	}
}

// readHostVars is the v2 declarations of the host_vars file at path.
func readHostVars(path string) (declare.Declaration, error) {
	data, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		return declare.Declaration{}, fmt.Errorf("read the host_vars: %w", err)
	}
	d, err := hostvars.Read(data)
	if err != nil {
		return declare.Declaration{}, fmt.Errorf("the host_vars %s: %w", path, err)
	}
	return d, nil
}
