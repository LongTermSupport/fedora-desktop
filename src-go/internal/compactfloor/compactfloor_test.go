package compactfloor

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestParse(t *testing.T) {
	if n, ok := Parse("150000").Tokens(); !ok || n != 150000 {
		t.Fatalf("got %d, %v", n, ok)
	}
	for _, bad := range []string{"", "0", "-5", "012", "1.5", "lots", "99999999999999999999"} {
		f := Parse(bad)
		if _, ok := f.Tokens(); ok {
			t.Errorf("%q should not be a floor", bad)
		}
		if f.String() != bad {
			t.Errorf("%q is reported as %q", bad, f.String())
		}
	}
}

// environment is a systemd that gives the set-going unit env, or fails with err.
func environment(env string, err error) func(context.Context, string) (string, error) {
	return func(_ context.Context, unit string) (string, error) {
		if unit != Unit {
			return "", errors.New("asked about " + unit)
		}
		return env, err
	}
}

func TestFromUnitReadsTheSetGoingUnitsEnvironment(t *testing.T) {
	floor, err := FromUnit(t.Context(), environment("LANG=C "+Env+"=120000 OTHER=x", nil))
	if err != nil {
		t.Fatal(err)
	}
	if n, ok := floor.Tokens(); !ok || n != 120000 || floor.String() != "120000" {
		t.Fatalf("got %d, %v, %q", n, ok, floor)
	}
}

func TestFromUnitFailsWithWhereTheFloorComesFrom(t *testing.T) {
	for _, tc := range []struct {
		name, env string
		err       error
		want      string
	}{
		{"not set", "", nil, Unit + " sets no " + Env},
		{"not tokens", Env + "=lots", nil, Env + " is 'lots' in " + Unit + ", not a whole number of tokens"},
		{"systemd fails", "", errors.New("no bus"), "the environment of " + Unit + " could not be read"},
	} {
		_, err := FromUnit(t.Context(), environment(tc.env, tc.err))
		if err == nil || !strings.Contains(err.Error(), tc.want) || !strings.Contains(err.Error(), "play-claude-yolo.yml") {
			t.Errorf("%s: %v", tc.name, err)
		}
	}
}
