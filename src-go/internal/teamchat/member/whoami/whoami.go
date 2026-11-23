// Package whoami is `teamchat whoami --json` (Plan 00209 PROPOSAL-seat-kinds.md section 4): the
// seats this session holds, read from the run's status.json alone. It makes no link call, reads
// no token, no seat store and no network, so a probe running as the session's user (the
// hooks-daemon's role probe) can ask "what is this session, per team?" without a secret.
//
// The output is a contract. stdout is one JSON object and a newline:
//
//	{"v":1,"teams":[{"team":"<team id>","handle":"<repo>.<seat>+<site>.<type>",
//	                 "role":"coordinator"|"worker"|"probe"|null,
//	                 "duty":"coordinator"|"worker"|"host"|null}]}
//
// one entry per held team whose handle the link knows, in status.json's order (by team id).
// `role` is null until the team's roster names one; `duty` is null where the host's desired
// membership gave the seat none, which a reader treats as "not known", never as worker. A
// session that holds no seat prints `{"v":1,"teams":[]}` and exits 0. The keys are exactly
// these; a new key is a new `v`.
//
// Exits: 0 printed; 69 `no-session` when TEAMCHAT_RUNTIME names no member run (not a teamchat
// session); 69 `no-link` when the run has no status.json yet (its link has not written one);
// 78 `runtime` when status.json is another run's or fails its strict decode. Each failure writes
// nothing to stdout and the error object to stderr (exits.Write).
package whoami

import (
	"errors"
	"io/fs"
	"path/filepath"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/cli"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/client"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/exits"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/homefs"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/state"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/proto"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

// Version is the output's `v`.
const Version = 1

const (
	msgNoStatus  exits.Template = "this session's teamchat has no status.json yet: its link has not started or written one; run teamchat status, and tell a human if it persists"
	msgBadStatus exits.Template = "this session's status.json is another run's or cannot be read; run teamchat status and tell a human"
)

// Main is the verb.
func Main(e cli.Env, _ cli.Invocation) term.ExitCode {
	out, err := read(e.Getenv)
	if err != nil {
		return exits.Write(e.Stderr, err)
	}
	if _, err := e.Stdout.Write(append(out, '\n')); err != nil {
		return exits.Write(e.Stderr, err)
	}
	return term.ExitOK
}

// read is the output's JSON for the session TEAMCHAT_RUNTIME names.
func read(getenv func(string) string) ([]byte, error) {
	run, err := client.Runtime(getenv)
	if err != nil {
		return nil, err
	}
	root, err := homefs.Open(run.Dir(), homefs.Private)
	if err != nil {
		return nil, exits.New(exits.CodeNoLink, nil, msgNoStatus)
	}
	st, readErr := state.Read(root, run.Status(), state.MaxStatus, state.DecodeStatus)
	if cerr := root.Close(); cerr != nil && readErr == nil {
		readErr = cerr
	}
	switch {
	case errors.Is(readErr, fs.ErrNotExist):
		return nil, exits.New(exits.CodeNoLink, nil, msgNoStatus)
	case readErr != nil || st.Run.String() != filepath.Base(run.Dir()):
		return nil, exits.New(exits.CodeRuntime, nil, msgBadStatus)
	}
	return encode(st), nil
}

// encode is the contract's object for st.
func encode(st state.Status) []byte {
	teams := make([]proto.Value, 0, len(st.Teams))
	for _, t := range st.Teams {
		if t.Handle == nil {
			continue
		}
		role, d := proto.Null(), proto.Null()
		if t.Role != nil {
			role = proto.Str(*t.Role)
		}
		if t.Duty != nil {
			d = proto.Str(string(*t.Duty))
		}
		teams = append(teams, proto.Obj(
			proto.M("team", proto.Str(t.Team.String())), proto.M("handle", proto.Str(*t.Handle)),
			proto.M("role", role), proto.M("duty", d)))
	}
	return proto.Obj(proto.M("v", proto.Int(Version)), proto.M("teams", proto.Arr(teams...))).Compact()
}
