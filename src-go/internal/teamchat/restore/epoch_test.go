package restore

import (
	"strings"
	"testing"
)

// RM10, n2g-D20: restore-epoch.json is strict {"v": 1, "epoch": n, "min_backup": n}.
func TestEpochFloorRoundTrips(t *testing.T) {
	f := EpochFloor{Epoch: 41, MinBackup: 9}
	raw := EncodeEpochFloor(f)
	if string(raw) != `{"v":1,"epoch":41,"min_backup":9}` {
		t.Errorf("encoded %s", raw)
	}
	got, err := DecodeEpochFloor(raw)
	if err != nil || got != f {
		t.Errorf("decoded %+v, %v", got, err)
	}
}

func TestEpochFloorDecodeRefuses(t *testing.T) {
	for name, raw := range map[string]string{
		"not json":     "{",
		"a version":    `{"v":2,"epoch":1,"min_backup":0}`,
		"a key":        `{"v":1,"epoch":1}`,
		"an extra":     `{"v":1,"epoch":1,"min_backup":0,"x":0}`,
		"a negative":   `{"v":1,"epoch":-1,"min_backup":0}`,
		"over the max": `{"v":1,"epoch":1000000000,"min_backup":0}`,
		"a string":     `{"v":1,"epoch":"1","min_backup":0}`,
		"too large":    `{"v":1,"epoch":1,"min_backup":0}` + strings.Repeat(" ", maxEpochFloor),
	} {
		if _, err := DecodeEpochFloor([]byte(raw)); err == nil {
			t.Errorf("%s: decoded", name)
		}
	}
}

// Raised only ever raises: each field the larger of the two.
func TestEpochFloorRaised(t *testing.T) {
	f := EpochFloor{Epoch: 5, MinBackup: 7}
	if got := f.Raised(EpochFloor{Epoch: 9, MinBackup: 2}); got != (EpochFloor{Epoch: 9, MinBackup: 7}) {
		t.Errorf("Raised = %+v", got)
	}
}
