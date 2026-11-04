package gitx

import (
	"context"
	"fmt"
	"strings"
)

// The release questions (Plan 00153 Task 5.3): which release tags exist, what was merged since
// the last one, which commits are release commits, and whether a release branch is already on
// the remote. Each asks git and reads its answer strictly: a git that cannot answer is an
// error, never "none", so a release is not decided on a listing that failed.

// unitSeparator divides the fields of a log record; a commit message cannot hold a NUL, so
// NUL ends a record (git log -z) and this divides its fields.
const unitSeparator = "\x1f"

// Tags is the tags git lists for the glob pattern.
func (g *Git) Tags(ctx context.Context, dir, pattern string) ([]string, error) {
	res, err := g.run(ctx, dir, "tag", "--list", pattern)
	if err != nil {
		return nil, err
	}
	return lines(res.Stdout), nil
}

// LogEntry is one commit of a log read for the number of its parents and its whole message.
type LogEntry struct {
	SHA     string
	Parents int
	Message string
}

// FirstParentLog is the commits in rev (a revision or a range) along the first parent only,
// newest first: a merged branch is the one merge commit, not its own commits.
func (g *Git) FirstParentLog(ctx context.Context, dir, rev string) ([]LogEntry, error) {
	res, err := g.run(ctx, dir, "log", "--first-parent", "-z", "--format=%H%x1f%P%x1f%B", rev)
	if err != nil {
		return nil, err
	}
	var out []LogEntry
	for record := range strings.SplitSeq(string(res.Stdout), "\x00") {
		if strings.TrimSpace(record) == "" {
			continue
		}
		fields := strings.SplitN(record, unitSeparator, 3)
		if len(fields) != 3 {
			return nil, fmt.Errorf("git log gave a record that is not sha, parents and message: %q", record)
		}
		out = append(out, LogEntry{
			SHA:     strings.TrimSpace(fields[0]),
			Parents: len(strings.Fields(fields[1])),
			Message: strings.TrimSpace(fields[2]),
		})
	}
	return out, nil
}

// Subject is a commit's sha and the first line of its message.
type Subject struct {
	SHA     string
	Subject string
}

// SubjectsMatching is the commits reachable from rev that git's --grep (an extended regular
// expression, matched against the whole message) selects, with their subjects. It is a
// pre-filter that keeps git from listing a long history; the caller judges each subject.
func (g *Git) SubjectsMatching(ctx context.Context, dir, rev, pattern string) ([]Subject, error) {
	res, err := g.run(ctx, dir, "log", "--extended-regexp", "--grep="+pattern, "-z", "--format=%H%x1f%s", rev)
	if err != nil {
		return nil, err
	}
	var out []Subject
	for record := range strings.SplitSeq(string(res.Stdout), "\x00") {
		if strings.TrimSpace(record) == "" {
			continue
		}
		sha, subject, ok := strings.Cut(record, unitSeparator)
		if !ok {
			return nil, fmt.Errorf("git log gave a record that is not sha and subject: %q", record)
		}
		out = append(out, Subject{SHA: strings.TrimSpace(sha), Subject: strings.TrimSpace(subject)})
	}
	return out, nil
}

// RemoteBranchExists is whether the remote (a name or a URL) has a branch called branch. A remote
// that cannot be reached is an error: it must not read as "no such branch".
func (g *Git) RemoteBranchExists(ctx context.Context, dir, remote, branch string) (bool, error) {
	res, err := g.run(ctx, dir, "ls-remote", "--heads", remote, branch)
	if err != nil {
		return false, err
	}
	ref := "refs/heads/" + branch
	for _, l := range lines(res.Stdout) {
		if _, name, ok := strings.Cut(l, "\t"); ok && name == ref {
			return true, nil
		}
	}
	return false, nil
}
