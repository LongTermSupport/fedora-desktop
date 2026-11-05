//go:build !busfailpoints

package failpoint

import (
	"strings"
	"testing"
)

// Without the tag every point is inert, even with every one named in the environment.
func TestHitIsInertWithoutTheTag(t *testing.T) {
	names := declared(t)
	t.Setenv("TEAMCHAT_FAILPOINTS", strings.Join(names, ","))
	for _, n := range names {
		Hit(Name(n))
	}
}
