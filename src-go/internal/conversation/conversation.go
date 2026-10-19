// Package conversation reads which Claude conversation a recorded session is in, so a restore
// after a reboot and a team restart resume that exact conversation by id, never with
// `--continue` (Plan 00203 SPEC-team-add-and-restart C1-C6, Decision 10).
//
// The pingbus SessionStart hook (helpers/pingbus/hooks.py) writes the id on every session
// start, so it follows a /clear, a resume and a compaction: `conversation.json` beside the
// seat keeper's report (membership.SessionDir), in the session's checkout as the host sees it.
// Only a launch that carried the agent team bus has the hook, so a record whose launch did not
// owns no file, and one left under its reused name is an earlier session's.
package conversation

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/registry"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/discover"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/membership"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/transcript"
)

// File is the hook's record in the session's directory.
const File = "conversation.json"

// maxBytes bounds what is read; the record is about 150 bytes.
const maxBytes = 4096

// sources are the SessionStart sources the hook names; any other is "other".
var sources = []string{"startup", "resume", "clear", "compact", "other"}

// recordV1 is the hook's record, field for field (SPEC C1).
type recordV1 struct {
	V         int    `json:"v"`
	Session   string `json:"session"`
	SessionID string `json:"session_id"`
	Source    string `json:"source"`
	WrittenMS *int64 `json:"written_ms"`
}

// ClaudeHome is where Claude Code keeps a host session's transcripts (cc's).
func ClaudeHome(getenv func(string) string) string {
	if dir := getenv("CLAUDE_CONFIG_DIR"); dir != "" {
		return dir
	}
	return getenv("HOME") + "/.claude"
}

// For is the conversation id the session of rec is to resume: the hook's record, refused
// unless rec's launch carried the bus, the record is exactly the hook's for this session with a
// Claude session id, and that conversation's own transcript is there (never the newest in the
// folder, which several sessions share). Every error names the session and how to resume it
// by hand. claudeHome is ClaudeHome's.
func For(rec registry.Record, claudeHome string) (string, error) {
	id, err := read(rec, claudeHome)
	if err != nil {
		return "", fmt.Errorf("%s: %w; resume it by hand: %s --resume in %s, whose picker lists the conversations",
			rec.Name, err, rec.Prefix, rec.Dir)
	}
	return id, nil
}

func read(rec registry.Record, claudeHome string) (string, error) {
	if !discover.LaunchedWithBus(rec) {
		return "", errors.New("it was launched without the agent team bus (ccy --no-bus, or a cc without --teams or --bus), so its conversation is not recorded")
	}
	checkout, err := checkoutOf(rec)
	if err != nil {
		return "", err
	}
	path := filepath.Join(membership.SessionDir(checkout, rec.Name), File)
	r, err := readRecord(path)
	if err != nil {
		return "", err
	}
	switch {
	case r.Session != rec.Name:
		return "", fmt.Errorf("%s names the session %q, not this one", path, r.Session)
	case !registry.SessionID(r.SessionID):
		return "", fmt.Errorf("%s holds %q, which is not a Claude session id", path, r.SessionID)
	}
	dir, err := transcript.Dir(rec.Prefix, rec.Dir, claudeHome)
	if err != nil {
		return "", err
	}
	if _, err := transcript.For(dir, r.SessionID); err != nil {
		return "", fmt.Errorf("the recorded conversation %s (%s) has no transcript: %w", r.SessionID, path, err)
	}
	return r.SessionID, nil
}

// checkoutOf is the checkout the session's hook wrote under: ccy's is its folder (mounted as
// /workspace), cc's the git checkout around its folder (cc-teams.bash takes the top level).
func checkoutOf(rec registry.Record) (string, error) {
	if rec.Prefix != membership.KindCC {
		return rec.Dir, nil
	}
	for dir := filepath.Clean(rec.Dir); ; dir = filepath.Dir(dir) {
		if _, err := os.Lstat(filepath.Join(dir, ".git")); err == nil {
			return dir, nil
		}
		if dir == filepath.Dir(dir) {
			return "", fmt.Errorf("%s is not in a git checkout, where cc records its conversation", rec.Dir)
		}
	}
}

func readRecord(path string) (recordV1, error) {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return recordV1{}, fmt.Errorf("no conversation is recorded (%s is not there)", path)
	}
	if err != nil {
		return recordV1{}, fmt.Errorf("could not read %s: %w", path, err)
	}
	if !info.Mode().IsRegular() {
		return recordV1{}, fmt.Errorf("%s is not a regular file", path)
	}
	f, err := os.Open(filepath.Clean(path))
	if err != nil {
		return recordV1{}, fmt.Errorf("could not read %s: %w", path, err)
	}
	data, err := io.ReadAll(io.LimitReader(f, maxBytes+1))
	if err = errors.Join(err, f.Close()); err != nil {
		return recordV1{}, fmt.Errorf("could not read %s: %w", path, err)
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	var r recordV1
	if err := dec.Decode(&r); err != nil || dec.More() || len(data) > maxBytes {
		return recordV1{}, fmt.Errorf("%s is not the SessionStart hook's record: %w", path, errors.Join(err, errNotARecord))
	}
	if r.V != 1 || r.WrittenMS == nil || *r.WrittenMS < 0 || !slices.Contains(sources, r.Source) {
		return recordV1{}, fmt.Errorf("%s is not a version 1 record of the SessionStart hook", path)
	}
	return r, nil
}

var errNotARecord = errors.New("not one version 1 record")
