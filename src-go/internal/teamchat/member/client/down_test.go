package client

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/exits"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/state"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
)

// Runtime is TEAMCHAT_RUNTIME's run, or 69 no-session for an unset or foreign one.
func TestRuntime(t *testing.T) {
	w := newWorld(t)
	r, err := Runtime(func(k string) string { return w.env[k] })
	if err != nil || r.Dir() != w.run.Dir() {
		t.Fatalf("run %v, err %v", r, err)
	}
	for _, dir := range []string{"", "relative/00112233445566ff", "/elsewhere/00112233445566ff"} {
		w.env[paths.EnvRuntime] = dir
		_, err := Runtime(func(k string) string { return w.env[k] })
		if e, ok := errors.AsType[*exits.Error](err); !ok || e.Code != exits.CodeNoSession {
			t.Fatalf("%q: err %v", dir, err)
		}
	}
}

// ReadDown is one keeper.json read and one keeper.lock probe, and never fails.
func TestReadDown(t *testing.T) {
	next := (boot0 + 2*time.Second).Milliseconds()
	for _, tc := range []struct {
		name   string
		keeper bool
		link   string
		kind   string
		next   *int64
		want   Down
	}{
		{"restarting", true, state.LinkRestarting, state.KindHost, &next, Down{State: DownRestarting, NextMS: next, Kind: state.KindHost}},
		{"running", true, state.LinkRunning, state.KindCCY, nil, Down{State: DownStarting, Kind: state.KindCCY, Session: "work-1"}},
		{"no keeper.json", true, "", "", nil, Down{State: DownStarting}},
		{"failed", true, state.LinkFailed, state.KindLXC, nil, Down{State: DownFailed, Kind: state.KindLXC, Session: "work-1"}},
		{"keeper gone", false, state.LinkRunning, state.KindHost, nil, Down{State: DownFailed, Kind: state.KindHost}},
		{"keeper gone, no keeper.json", false, "", "", nil, Down{State: DownFailed}},
	} {
		w := newWorld(t)
		if tc.keeper {
			w.keeper()
		}
		if tc.link != "" {
			w.keeperJSON(tc.link, tc.kind, tc.next)
		}
		if got := ReadDown(w.run); got != tc.want {
			t.Fatalf("%s: %+v, want %+v", tc.name, got, tc.want)
		}
	}
	// a run directory that is not there is a failed link.
	w := newWorld(t)
	if got := ReadDown(paths.CCYLayout().Run(w.runID())); got.State != DownFailed {
		t.Fatalf("no run directory: %+v", got)
	}
}

// Down.Error is LN3's 69, as the verbs give it.
func TestDownError(t *testing.T) {
	team, err := paths.ParseTeamID("alpha")
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		d     Down
		code  string
		want  string
		other string
	}{
		{Down{State: DownRestarting, NextMS: (boot0 + 4200*time.Millisecond).Milliseconds()}, exits.CodeNoLink, "next try in 5 s", ""},
		{Down{State: DownRestarting, NextMS: (boot0 - time.Second).Milliseconds()}, exits.CodeNoLink, "next try in 0 s", ""},
		{Down{State: DownStarting}, exits.CodeNoLink, "is starting or restarting", "next try"},
		{Down{State: DownFailed, Kind: state.KindCCY, Session: "work-1"}, exits.CodeLinkFailed, "`ccy-teams restart work-1`", ""},
		{Down{State: DownFailed, Kind: state.KindLXC, Session: "work-1"}, exits.CodeLinkFailed, "`ccy-teams restart work-1`", ""},
		{Down{State: DownFailed, Kind: state.KindHost}, exits.CodeLinkFailed, "launch it again", "ccy-teams"},
		{Down{State: DownFailed}, exits.CodeLinkFailed, "launch it again", "ccy-teams"},
		{Down{State: "unknown"}, exits.CodeLinkFailed, "launch it again", ""},
	} {
		e := tc.d.Error(&team, boot0)
		if e.Code != tc.code || !strings.Contains(e.Message, tc.want) || tc.other != "" && strings.Contains(e.Message, tc.other) {
			t.Fatalf("%+v: %+v", tc.d, e)
		}
		if e.Team == nil || *e.Team != team {
			t.Fatalf("%+v: team %v", tc.d, e.Team)
		}
	}
	if e := (Down{State: DownStarting}).Error(nil, boot0); e.Team != nil {
		t.Fatalf("no team: %+v", e)
	}
}
