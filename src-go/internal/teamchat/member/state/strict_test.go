package state

import (
	"context"
	"errors"
	"testing"
	"time"
)

// noSleep is a lock clock whose waits end at once.
type noSleep struct{}

func (noSleep) Sleep(context.Context, time.Duration) error { return nil }

type sample struct {
	V     int      `json:"v"`
	Names []string `json:"names"`
	Opt   *int64   `json:"opt,omitempty"`
}

func (s sample) check(c *rules) {
	c.need(s.Names != nil, "names is not a list")
}

func TestDecodeStrict(t *testing.T) {
	var s sample
	if _, err := decodeStrict("sample", []byte(`{"v":1,"names":["a"]}`), 64, &s, func(c *rules) { s.check(c) }); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{
		`{"v":1,"names":null}`,
		`{"v":1,"names":["a"],"opt":null}`,
		`{"v":1,"names":[1]}`,
		`{"v":1,"names":["a"],"NAMES":["b"]}`,
		`{"v":1,"names":["` + "\xff" + `"]}`,
		`{"v":1,"names":["a"]}x`,
		`{"v":1,"names":["a","a","a","a","a","a","a","a","a","a","a","a","a","a","a","a","a","a","a","a"]}`,
	} {
		var got sample
		_, err := decodeStrict("sample", []byte(bad), 64, &got, func(c *rules) { got.check(c) })
		if !errors.Is(err, ErrInvalid) {
			t.Errorf("decodeStrict(%q) = %v, want ErrInvalid", bad, err)
		}
	}
}

func TestRulesKeepTheFirst(t *testing.T) {
	c := &rules{}
	c.need(true, "one")
	c.need(false, "two")
	c.need(false, "three")
	if c.rule != "two" {
		t.Fatalf("rule = %q", c.rule)
	}
}

func TestInvalidErrorIs(t *testing.T) {
	err := error(&InvalidError{File: "f", Code: CodeSchema, Rule: "r"})
	if !errors.Is(err, ErrInvalid) || errors.Is(err, ErrFull) || err.Error() != "state: f is invalid (schema): r" {
		t.Fatalf("InvalidError = %v", err)
	}
}
