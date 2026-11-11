// Package qaverdict is qa-all's verdict on a commit: whether the run passed, the commit it
// judged (HEAD when it started) and when it started and ended. qa-all writes it to FileName in
// the repository's git common directory, shared by every worktree of the checkout, as a local
// record of the last run read as data rather than from its printed text. It gates nothing: a
// push to a protected branch is gated on CI's run of the commit (qa/pushgate).
package qaverdict

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/qa/internal/proc"
)

// FileName is the verdict's name in the git common directory.
const FileName = "qa-all-verdict.json"

// The two statuses a finished run records.
const (
	StatusPass = "pass"
	StatusFail = "fail"
)

// fileMode lets another account of the machine (the host beside a container) read it.
const fileMode = 0o644

// ErrNone is a checkout with no verdict: no qa-all run has finished since the last one started.
var ErrNone = errors.New("no qa-all verdict")

var objectID = regexp.MustCompile(`^([0-9a-f]{40}|[0-9a-f]{64})$`)

// Record is one finished run's verdict.
type Record struct {
	Status   string    `json:"status"`
	Commit   string    `json:"commit"`
	Started  time.Time `json:"started"`
	Finished time.Time `json:"finished"`
}

func (r Record) check() error {
	switch {
	case r.Status != StatusPass && r.Status != StatusFail:
		return fmt.Errorf("status %q is neither %q nor %q", r.Status, StatusPass, StatusFail)
	case !objectID.MatchString(r.Commit):
		return fmt.Errorf("commit %q is not an object id", r.Commit)
	case r.Started.IsZero() || r.Finished.IsZero():
		return errors.New("a start or end time is missing")
	case r.Finished.Before(r.Started):
		return errors.New("the run ends before it starts")
	}
	return nil
}

// Target is what a run in a checkout judges and where its verdict goes.
type Target struct {
	Commit string
	Path   string
}

// Locate asks git for the checkout's HEAD commit and its git common directory.
func Locate(ctx context.Context, runner proc.Runner, dir string) (Target, error) {
	head, err := ask(ctx, runner, dir, "rev-parse", "--verify", "--quiet", "HEAD^{commit}")
	if err != nil {
		return Target{}, fmt.Errorf("cannot name the commit qa-all judges: %w", err)
	}
	if !objectID.MatchString(head) {
		return Target{}, fmt.Errorf("git named HEAD %q, not an object id", head)
	}
	common, err := ask(ctx, runner, dir, "rev-parse", "--path-format=absolute", "--git-common-dir")
	if err != nil {
		return Target{}, fmt.Errorf("cannot find where the qa-all verdict goes: %w", err)
	}
	if !filepath.IsAbs(common) {
		return Target{}, fmt.Errorf("git named the common directory %q, not an absolute path", common)
	}
	return Target{Commit: head, Path: filepath.Join(common, FileName)}, nil
}

func ask(ctx context.Context, runner proc.Runner, dir string, args ...string) (string, error) {
	res, err := runner.Run(ctx, proc.Cmd{Argv: append([]string{"git", "-C", dir}, args...)})
	if err != nil {
		return "", fmt.Errorf("git: %w", err)
	}
	if res.Code != 0 {
		return "", fmt.Errorf("git %s in %s: exit %d: %s", args[0], dir, res.Code, strings.TrimSpace(res.Stderr))
	}
	return strings.TrimSpace(res.Stdout), nil
}

// Remove clears the verdict, so a run that ends without one leaves none to be read as its own.
func Remove(path string) error {
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("cannot clear the previous qa-all verdict %s: %w", path, err)
	}
	return nil
}

// Write replaces the verdict in one step (written beside it and renamed), refusing a record
// that does not say what it judged.
func Write(path string, rec Record) (err error) {
	if bad := rec.check(); bad != nil {
		return fmt.Errorf("refusing to write the qa-all verdict: %w", bad)
	}
	body, err := json.MarshalIndent(rec, "", "  ")
	if err != nil {
		return fmt.Errorf("cannot encode the qa-all verdict: %w", err)
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".qa-all-verdict-*")
	if err != nil {
		return fmt.Errorf("cannot create the qa-all verdict beside %s: %w", path, err)
	}
	defer func() {
		if err != nil {
			err = errors.Join(err, os.Remove(f.Name()))
		}
	}()
	if _, err = f.Write(append(body, '\n')); err != nil {
		return errors.Join(fmt.Errorf("cannot write %s: %w", f.Name(), err), f.Close())
	}
	if err = f.Close(); err != nil {
		return fmt.Errorf("cannot close %s: %w", f.Name(), err)
	}
	if err = os.Chmod(f.Name(), fileMode); err != nil {
		return fmt.Errorf("cannot set the mode of %s: %w", f.Name(), err)
	}
	if err = os.Rename(f.Name(), path); err != nil {
		return fmt.Errorf("cannot move the qa-all verdict into place at %s: %w", path, err)
	}
	return nil
}

// Read loads the verdict: ErrNone when there is none, an error for one that is damaged.
func Read(path string) (Record, error) {
	body, err := os.ReadFile(filepath.Clean(path))
	if errors.Is(err, os.ErrNotExist) {
		return Record{}, ErrNone
	}
	if err != nil {
		return Record{}, fmt.Errorf("cannot read the qa-all verdict %s: %w", path, err)
	}
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.DisallowUnknownFields()
	var rec Record
	if err := dec.Decode(&rec); err != nil {
		return Record{}, fmt.Errorf("the qa-all verdict %s is damaged: %w", path, err)
	}
	if err := rec.check(); err != nil {
		return Record{}, fmt.Errorf("the qa-all verdict %s is damaged: %w", path, err)
	}
	return rec, nil
}
