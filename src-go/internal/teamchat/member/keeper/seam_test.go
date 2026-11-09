package keeper

import (
	"errors"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/exits"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
)

// internalFault fails t unless err is the keeper's internal fault.
func internalFault(t *testing.T, err error) {
	t.Helper()
	var e *exits.Error
	if !errors.As(err, &e) || e.Code != exits.CodeInternal {
		t.Fatalf("err %v, want the internal fault", err)
	}
}

// Only cli.ParseLaunch makes a launch that reaches prepare, and it always names an agent; a
// launch that names none is refused before anything is made.
func TestALaunchNamingNoAgentIsRefusedFirst(t *testing.T) {
	k := &keeper{}
	err := k.prepare()
	internalFault(t, err)
	if want := "the keeper could not resolve the agent of the launch"; err.Error() != want {
		t.Errorf("message %q, want %q", err, want)
	}
	if k.runRoot != nil || len(k.early) != 1 {
		t.Fatalf("run root %v, log %q", k.runRoot, k.early)
	}
}

// An adapter writes only files directly under the run.
func TestAnAdapterWritesNoFileOutsideTheRun(t *testing.T) {
	layout, err := paths.HomeLayout("/home/u", "/run/user/1")
	mustDo(t, err)
	id, err := paths.ParseRunID("0123456789abcdef")
	mustDo(t, err)
	k := &keeper{run: layout.Run(id)}
	dir := k.run.Dir()
	for _, p := range []string{dir + "/sub/settings.json", dir + "/../settings.json", dir + "//settings.json", dir, "/home/u/settings.json"} {
		internalFault(t, k.writeRunFile(p, []byte("{}")))
	}
}
