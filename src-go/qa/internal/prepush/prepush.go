// Package prepush reads what git hands a pre-push hook: one "LOCALREF LOCALID REMOTEREF
// REMOTEID" line per ref on stdin, and selects the commits of a ref the remote does not hold.
// The private-names scan and the push gate share it, so the two hooks judge the same commits.
package prepush

import (
	"errors"
	"fmt"
	"io"
	"strings"
)

// ZeroID is the all-zero object id: a local one is a deletion, a remote one a ref the remote
// does not hold yet.
const ZeroID = "0000000000000000000000000000000000000000"

// Ref is one pre-push ref line.
type Ref struct {
	LocalRef, LocalID, RemoteRef, RemoteID string
}

// Deletes is whether the push deletes the remote ref, which pushes no commit.
func (r Ref) Deletes() bool { return r.LocalID == ZeroID }

// Parse reads the ref lines, refusing one without four fields.
func Parse(in io.Reader) ([]Ref, error) {
	if in == nil {
		return nil, errors.New("a pre-push run needs the ref lines on stdin")
	}
	data, err := io.ReadAll(in)
	if err != nil {
		return nil, fmt.Errorf("reading the pre-push ref lines: %w", err)
	}
	var refs []Ref
	for line := range strings.SplitSeq(string(data), "\n") {
		f := strings.Fields(line)
		if len(f) == 0 {
			continue
		}
		if len(f) != 4 {
			return nil, errors.New("a pre-push ref line does not have four fields")
		}
		refs = append(refs, Ref{LocalRef: f[0], LocalID: f[1], RemoteRef: f[2], RemoteID: f[3]})
	}
	return refs, nil
}

// Selection is the git revision selection of the ref's commits the remote does not already
// hold: one under none of that remote's tracking refs and not behind its own old tip. A branch's
// old tip alone is not enough (a commit another branch published since is already public), and
// another remote's refs are no proof (refs/remotes/ can hold local archives). Pushed to a URL,
// which has no tracking refs, an existing branch is selected from its old tip and a new one
// against every tracking ref.
func (r Ref) Selection(remote string) []string {
	sel := []string{r.LocalID, "--not"}
	named := remote != "" && !strings.ContainsAny(remote, ":/")
	if named {
		sel = append(sel, "--remotes="+remote)
	}
	if r.RemoteID != ZeroID {
		return append(sel, r.RemoteID)
	}
	if !named {
		sel = append(sel, "--remotes")
	}
	return sel
}
