package changed

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"

	"github.com/LongTermSupport/fedora-desktop/src-go/qa/internal/qatrees"
)

// rowComparers are the files qa-changed can compare by the rows their readers parse, each with
// its comparison: whether base and work hold the same rows, or an error when either does not
// parse. A comments row of the routing table names only these files (issue #413).
var rowComparers = map[string]func(base, work []byte) (bool, error){
	qatrees.File: sameTreeRows,
}

// sameTreeRows compares two texts of the trees table as qatrees.Load reads it.
func sameTreeRows(base, work []byte) (bool, error) {
	before, err := qatrees.Parse(bytes.NewReader(base), "the base's "+qatrees.File)
	if err != nil {
		return false, err
	}
	after, err := qatrees.Parse(bytes.NewReader(work), qatrees.File)
	if err != nil {
		return false, err
	}
	return slices.Equal(before.Shell, after.Shell) && slices.Equal(before.Python, after.Python), nil
}

// rowsComparable is whether every file the matcher matches is one qa-changed can compare: a glob
// list of literal paths, each with a comparison.
func rowsComparable(m matcher) bool {
	if m.kind != kindGlob {
		return false
	}
	return !slices.ContainsFunc(m.globs, func(g string) bool {
		_, ok := rowComparers[g]
		return !ok
	})
}

// commentRows is the comments rows that route rel, when its change is to its comments and blank
// lines alone; nil when it has none, or when its rows changed. Any doubt is a change to the rows,
// with a note saying why, so the file is planned every check its rows decide: an explicit list
// (no base), a base git cannot show (a new file, a bad ref) or a text that does not parse.
func (s *session) commentRows(ctx context.Context, rel string) ([]rule, error) {
	var rows []rule
	for _, r := range s.routing.comments {
		match, err := r.m.matches(rel, s.probe, s.listed)
		if err != nil {
			return nil, err
		}
		if match {
			rows = append(rows, r)
		}
	}
	if len(rows) == 0 {
		return nil, nil
	}
	whole := func(why string) ([]rule, error) {
		s.plan.notes = append(s.plan.notes, fmt.Sprintf("%s: %s, so every check its rows decide runs", rel, why))
		return nil, nil
	}
	if s.opts.explicit {
		return whole("an explicit file list has no base to compare its rows with")
	}
	base, err := s.git(ctx, "show", s.opts.base+":"+rel)
	if err != nil {
		return whole(fmt.Sprintf("cannot read it at %s (%v)", s.opts.base, err))
	}
	work, err := os.ReadFile(filepath.Join(s.root, rel))
	if err != nil {
		return nil, fmt.Errorf("cannot read %s: %w", rel, err)
	}
	same, err := rowComparers[rel]([]byte(base), work)
	if err != nil {
		return whole(fmt.Sprintf("cannot compare its rows with %s (%v)", s.opts.base, err))
	}
	if !same {
		return nil, nil
	}
	s.plan.commentsOnly = append(s.plan.commentsOnly, rel)
	s.plan.notes = append(s.plan.notes, fmt.Sprintf("%s: only its comments or blank lines changed (its rows are those at %s), so only its comments rows run", rel, s.opts.base))
	return rows, nil
}
