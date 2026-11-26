package conversation

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/registry"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/membership"
)

// RetiredFile is the name a retired agent-bus conversation record is moved to, beside where it
// was: nothing reads it as a record, and its id stays there for a person.
const RetiredFile = "conversation.retired.json"

// Retire moves session's agent-bus conversation record in checkout aside (to RetiredFile), so a
// restore does not take it as a conversation some session may own under another name
// (fedora-desktop#431, recordedElsewhere): the way out once the session that wrote it was
// forgotten or ended on purpose. It returns the record's path and whether it was retired: false,
// with no error, when there is no record there (or no checkout). The move is made through an os.Root on the checkout, and
// only when every directory from .claude down and the record itself are real (a link is
// refused, never followed: a container may write .claude/ccy).
func Retire(checkout, session string) (path string, retired bool, err error) {
	if !membership.ValidSession(session) {
		return "", false, fmt.Errorf("%q is not a session name", session)
	}
	probe := registry.Record{Name: session, Dir: checkout, Prefix: membership.KindCcy, Args: busArgs(membership.KindCcy)}
	loc, err := Locate(probe, func(string) string { return "" })
	if err != nil {
		return "", false, err
	}
	path = loc.Path
	rel, err := filepath.Rel(checkout, path)
	if err != nil {
		return "", false, fmt.Errorf("the conversation record %s: %w", path, err)
	}
	root, err := os.OpenRoot(checkout)
	if errors.Is(err, fs.ErrNotExist) {
		return path, false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("the conversation record %s: %w", path, err)
	}
	defer func() {
		if closeErr := root.Close(); closeErr != nil {
			err = errors.Join(err, fmt.Errorf("closing %s: %w", checkout, closeErr))
		}
	}()
	parts := strings.Split(filepath.Dir(rel), string(filepath.Separator))
	for i := range parts {
		sub := filepath.Join(parts[:i+1]...)
		info, statErr := root.Lstat(sub)
		switch {
		case errors.Is(statErr, fs.ErrNotExist):
			return path, false, nil
		case statErr != nil:
			return "", false, fmt.Errorf("the conversation record %s: %w", path, statErr)
		case !info.IsDir():
			return "", false, fmt.Errorf("the conversation record %s is left as it is: %s is not a directory (a link is never followed)",
				path, filepath.Join(checkout, sub))
		}
	}
	info, err := root.Lstat(rel)
	if errors.Is(err, fs.ErrNotExist) {
		return path, false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("the conversation record %s: %w", path, err)
	}
	if !info.Mode().IsRegular() {
		return "", false, fmt.Errorf("the conversation record %s is left as it is: it is not a regular file (a link is never followed)", path)
	}
	if err := root.Rename(rel, filepath.Join(filepath.Dir(rel), RetiredFile)); err != nil {
		return "", false, fmt.Errorf("could not retire the conversation record %s: %w", path, err)
	}
	return path, true, nil
}

// RetireOwn is Retire of rec's own agent-bus record, in the checkout its hook wrote under (a cc
// record's git top): what forgetting the session does, since a record nothing owns would stop
// every later fresh start in the checkout. A folder that is gone, or no record there, has nothing
// to retire: "" and no error.
func RetireOwn(rec registry.Record) (string, error) {
	if _, err := os.Lstat(rec.Dir); errors.Is(err, fs.ErrNotExist) {
		return "", nil
	}
	checkout, err := checkoutOf(rec)
	if err != nil {
		return "", err
	}
	path, retired, err := Retire(checkout, rec.Name)
	if err != nil || !retired {
		return "", err
	}
	return path, nil
}
