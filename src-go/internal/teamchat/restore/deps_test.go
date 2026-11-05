package restore

import "testing"

// RS18: stopped is inactive, failed or not loaded; activating and every other state is not.
func TestStopped(t *testing.T) {
	for _, c := range []struct {
		st   UnitState
		want bool
	}{
		{UnitState{Load: "loaded", Active: "inactive"}, true},
		{UnitState{Load: "loaded", Active: "failed"}, true},
		{UnitState{Load: "not-found", Active: "inactive"}, true},
		{UnitState{Load: "not-found", Active: "active"}, true},
		{UnitState{Load: "loaded", Active: "activating"}, false},
		{UnitState{Load: "loaded", Active: "active"}, false},
		{UnitState{Load: "loaded", Active: "deactivating"}, false},
		{UnitState{Load: "loaded", Active: "reloading"}, false},
	} {
		if got := c.st.Stopped(); got != c.want {
			t.Errorf("%+v stopped = %v, want %v", c.st, got, c.want)
		}
	}
	if !(UnitState{Load: "not-found"}).NotLoaded() || (UnitState{Load: "loaded"}).NotLoaded() {
		t.Error("NotLoaded is not Load not-found")
	}
}
