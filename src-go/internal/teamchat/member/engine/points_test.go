package engine

import (
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/failpoint"
)

// Every engine point names its own failpoint, so a hit reaches the point PT1 arms.
func TestEachPointNamesItsFailpoint(t *testing.T) {
	want := map[point]failpoint.Name{
		pointAfterHandOut:      failpoint.LinkAfterHandOut,
		pointAfterAuth:         failpoint.LinkAfterAuth,
		pointAfterAwaiting:     failpoint.LinkAfterAwaiting,
		pointAfterUserLine:     failpoint.LinkAfterUserLine,
		pointBetweenStoreWrite: failpoint.LinkBetweenStoreWrites,
		pointAfterMissing:      failpoint.LinkAfterMissing,
	}
	for p, n := range want {
		if got := p.name(); got != n {
			t.Errorf("point %d names %q, want %q", p, got, n)
		}
	}
	if got := point(-1).name(); got != "" {
		t.Errorf("an unknown point names %q", got)
	}
	hitPoint(pointAfterAuth)
}
