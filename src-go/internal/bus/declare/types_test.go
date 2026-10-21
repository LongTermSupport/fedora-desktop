package declare

import "testing"

// The words are the ones declare.py prints and internal/declare already returns, so that
// internal/declare can alias these types and its callers do not change.
func TestTheStatesAreDeclarePysWords(t *testing.T) {
	for _, c := range [][2]string{
		{string(DescriptionVaulted), "vaulted"},
		{string(DescriptionInvalid), "invalid"},
		{string(SettingVaulted), "vaulted"},
		{string(SettingInvalid), "invalid"},
		{string(StandingCovers), "covers"},
		{string(StandingWiden), "widen"},
		{string(StandingExcluded), "excluded"},
		{string(StandingUnreadable), "unreadable"},
	} {
		if c[0] != c[1] {
			t.Errorf("%q, want %q", c[0], c[1])
		}
	}
}
