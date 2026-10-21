package statusline

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
)

// S21: limitBounds is limits.BOUNDS, read from this checkout's Python on every run, so a key the
// Python adds (Plan 00211's report_per_hour) fails here instead of the Go refusing a bundle the
// Python accepts.
func TestLimitBoundsAreThePythonsOwn(t *testing.T) {
	script := "import json, sys; sys.path.insert(0, sys.argv[1]); from helpers.pingbus import limits; " +
		"print(json.dumps({k: list(v) for k, v in limits.BOUNDS.items()}))"
	cmd := execx.Cmd{Name: "python3", Args: []string{"-I", "-c", script, checkoutOf(t)}}
	res, err := (execx.OS{}).Run(context.Background(), cmd)
	if err != nil {
		t.Fatalf("the Python bounds: %v", err)
	}
	var python map[string][2]int64
	if err := json.Unmarshal(res.Stdout, &python); err != nil {
		t.Fatalf("the Python bounds %q: %v", res.Stdout, err)
	}
	if !reflect.DeepEqual(python, limitBounds) {
		t.Errorf("limitBounds = %v, the Python's limits.BOUNDS = %v", limitBounds, python)
	}
}
