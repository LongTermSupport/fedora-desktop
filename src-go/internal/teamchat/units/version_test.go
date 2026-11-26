package units

import (
	"errors"
	"testing"

	"github.com/godbus/dbus/v5"
)

// The systemd version the restore proof is keyed to (issue #324) is the Manager's own Version
// property, asked over the bus as every other systemd question of this package is (systemctl is
// internal/systemd's): the package's version when the build names one ("256.7-1.fc41"), else
// the bare number. Nothing is run.
func TestSystemdVersion(t *testing.T) {
	for _, want := range []string{"256.7-1.fc41", "257", "258~rc1"} {
		r := newRig(t)
		r.conn.on(propertiesGet, func(fakeCall) ([]any, error) { return []any{dbus.MakeVariant(want)}, nil })
		got, err := r.m.SystemdVersion(t.Context())
		if err != nil || got != want {
			t.Errorf("SystemdVersion = %q, %v, want %q", got, err, want)
		}
		g := r.conn.call(t, propertiesGet)
		if g.dest != destination || g.path != managerPath || len(g.args) != 2 || g.args[0] != managerIface || g.args[1] != "Version" {
			t.Errorf("get = %+v", g)
		}
		if c := r.run.Calls(); len(c) != 0 {
			t.Errorf("ran %+v", c)
		}
	}
}

// A version that is not one, a reply that is not a string, and a bus that fails are errors,
// never a key.
func TestSystemdVersionRefuses(t *testing.T) {
	for name, answer := range map[string]answerFunc{
		"empty":          func(fakeCall) ([]any, error) { return []any{dbus.MakeVariant("")}, nil },
		"not a version":  func(fakeCall) ([]any, error) { return []any{dbus.MakeVariant("systemd 256")}, nil },
		"not a string":   func(fakeCall) ([]any, error) { return []any{dbus.MakeVariant(uint32(256))}, nil },
		"not a variant":  func(fakeCall) ([]any, error) { return []any{"256"}, nil },
		"a refusal":      func(fakeCall) ([]any, error) { return nil, dbus.Error{Name: "org.freedesktop.DBus.Error.AccessDenied"} },
		"a lost request": func(fakeCall) ([]any, error) { return nil, errors.New("gone") },
	} {
		r := newRig(t)
		r.conn.on(propertiesGet, answer)
		if got, err := r.m.SystemdVersion(t.Context()); err == nil {
			t.Errorf("%s: SystemdVersion = %q", name, got)
		}
	}
	r := newRig(t)
	r.dialErr = errors.New("no bus")
	if got, err := r.m.SystemdVersion(t.Context()); !errors.Is(err, ErrUnreachable) {
		t.Errorf("no bus: SystemdVersion = %q, %v", got, err)
	}
}
