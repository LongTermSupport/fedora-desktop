package cli

import (
	"testing"

	"golang.org/x/sys/unix"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/exits"
)

// guest check is built and reaches the real guest seam: the test's own caller is root or is
// not `agent`, so it is refused as the caller (GU1), 78 identity, with nothing on stdout.
func TestGuestCheckIsBuilt(t *testing.T) {
	old := unix.Umask(0o022)
	t.Cleanup(func() { unix.Umask(old) })
	var w world
	if w.run(digest, "guest", "check", "--user", "agent", "--team", "alpha") != exits.ExitConfig || w.out.Len() != 0 {
		t.Fatalf("stdout %q stderr %q", w.out.String(), w.err.String())
	}
	if e := w.lastError(t); e.Code != exits.CodeIdentity {
		t.Errorf("%+v", e)
	}
}

func TestGuestCheckRefusesAnInvocationWithoutItsFlags(t *testing.T) {
	var w world
	if guestCheck(Env{Stderr: &w.err}, Invocation{}) != exits.ExitUsage {
		t.Fatalf("stderr %q", w.err.String())
	}
}
