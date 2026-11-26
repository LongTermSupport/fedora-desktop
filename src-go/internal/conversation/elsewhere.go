package conversation

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/registry"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/discover"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/membership"
)

// elsewhere is a conversation record of rec's checkout under another session name (or under its
// own name on the other system): the session it names, the record's path, and its conversation
// id, or why the record could not be read.
type elsewhere struct {
	name, path, id string
	err            error
}

// recordedElsewhere is every agent-bus conversation record of rec's checkout that may be rec's
// own under an earlier name (fedora-desktop#431): a rename, or a move of its teams to teamchat,
// changes the key a restore finds the conversation by, and leaves the record under the old one.
// A record counts when it names a conversation whose transcript is here (one that never took a
// turn has nothing to resume) or cannot be read (it might be rec's); one that another session
// record of the registry owns is that session's, never rec's, and the path rec's own lookup read
// is not elsewhere. A teamchat session's own state under another name is not searched: its clean
// end carries its conversation into its seats' records, which StartPlan reads.
func recordedElsewhere(rec registry.Record, where Where) ([]elsewhere, error) {
	own, err := Locate(rec, where.Getenv)
	if err != nil {
		return nil, err
	}
	checkout, err := checkoutOf(rec)
	if err != nil {
		return nil, err
	}
	owned, err := ownedNames(rec.Name, where.Getenv)
	if err != nil {
		return nil, err
	}
	dir := membership.SessionDir(checkout, "")
	entries, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("could not list the session records of %s: %w", dir, err)
	}
	var found []elsewhere
	for _, e := range entries {
		if !e.IsDir() || !membership.ValidSession(e.Name()) || owned[e.Name()] {
			continue
		}
		probe := registry.Record{Name: e.Name(), Dir: rec.Dir, Prefix: rec.Prefix, Args: busArgs(rec.Prefix)}
		loc, err := Locate(probe, where.Getenv)
		if err != nil {
			return nil, err
		}
		if loc.Path == own.Path {
			continue
		}
		r, err := readRecorded(probe, where)
		switch {
		case errors.Is(err, errNotRecorded), errors.Is(err, errNoTranscript):
			continue
		case err != nil:
			found = append(found, elsewhere{name: e.Name(), path: loc.Path, err: err})
		default:
			found = append(found, elsewhere{name: e.Name(), path: loc.Path, id: r.id})
		}
	}
	return found, nil
}

// busArgs is a launch on the agent team bus with no team, for a probe Locate reads as agent-bus:
// ccy carries the bus unless told not to, cc only when asked.
func busArgs(prefix string) []string {
	if prefix == membership.KindCC {
		return []string{"--bus"}
	}
	return nil
}

// ownedNames is the names of the registry's session records other than self, and the file
// names of those it cannot read: a conversation recorded under one is that session's.
func ownedNames(self string, getenv func(string) string) (map[string]bool, error) {
	dir, err := registry.Dir(getenv)
	if err != nil {
		return nil, err
	}
	reading, err := registry.New(dir).ReadAll()
	if err != nil {
		return nil, err
	}
	owned := map[string]bool{}
	for _, r := range reading.Records {
		owned[r.Name] = r.Name != self
	}
	for _, p := range reading.Problems {
		if name := filepath.Base(p.File); name != self {
			owned[name] = true
		}
	}
	return owned, nil
}

// elsewhereError is StartPlan's refusal of rec, whose own lookup found nothing (err), while its
// checkout records the conversations found under other names: each is named, with how to resume
// it as rec's, and how to start rec fresh on purpose.
func elsewhereError(rec registry.Record, err error, found []elsewhere) error {
	lines := make([]string, 0, len(found))
	for _, f := range found {
		if f.err != nil {
			lines = append(lines, fmt.Sprintf("  %s: %s cannot be read: %v", f.name, f.path, f.err))
			continue
		}
		lines = append(lines, fmt.Sprintf("  %s: conversation %s (%s)", f.name, f.id, f.path))
	}
	id := "<id>"
	if len(found) == 1 && found[0].err == nil {
		id = found[0].id
	}
	list := discover.RecordTeams(rec)
	resume := fmt.Sprintf("  Resume it as %s's: %s --resume %s in %s, and the session records it under its own name", rec.Name, rec.Prefix, id, rec.Dir)
	fresh := fmt.Sprintf("  If none is %s's: start it once by hand in %s (%s), and its own record answers every later restore", rec.Name, rec.Dir, rec.Prefix)
	if list != "" {
		resume = fmt.Sprintf("  Resume it as %s's: ccy-teams seats conversation %s --set %s, then ccy-sessions restore %s", rec.Name, list, id, rec.Name)
		fresh = fmt.Sprintf("  If none is %s's: start it once by hand in %s: %s --teams %s --fresh", rec.Name, rec.Dir, rec.Prefix, list)
	}
	return fmt.Errorf("%s: %w; it is not started fresh: this checkout records a conversation under another session name, "+
		"which a rename (or a move of its teams to teamchat) leaves behind, so it may be this session's:\n%s\n%s\n%s",
		rec.Name, err, strings.Join(lines, "\n"), resume, fresh)
}
