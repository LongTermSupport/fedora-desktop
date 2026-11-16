package units

import (
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
)

// The systemd version the restore proof is keyed to (issue #324) is the first line of
// `systemctl --version`, asked through execx: the package's own version in parentheses, or the
// bare number when systemd prints none.
func TestSystemdVersion(t *testing.T) {
	for out, want := range map[string]string{
		"systemd 256 (256.7-1.fc41)\n+PAM +AUDIT\n": "256.7-1.fc41",
		"systemd 257\n+PAM\n":                       "257",
	} {
		r := newRig(t)
		r.run.On([]string{"/usr/bin/systemctl", "--version"}, execx.Result{Stdout: []byte(out)})
		got, err := r.m.SystemdVersion(t.Context())
		if err != nil || got != want {
			t.Errorf("%q: SystemdVersion = %q, %v, want %q", out, got, err, want)
		}
		if c := r.run.Calls(); len(c) != 1 || !c[0].CleanEnv || r.dials != 0 {
			t.Errorf("%q: ran %+v, dials %d", out, c, r.dials)
		}
	}
	for name, res := range map[string]execx.Result{
		"a failure":    {ExitCode: 1},
		"no output":    {},
		"another head": {Stdout: []byte("udev 256\n")},
		"no number":    {Stdout: []byte("systemd (x)\n")},
	} {
		r := newRig(t)
		r.run.On([]string{"/usr/bin/systemctl", "--version"}, res)
		if got, err := r.m.SystemdVersion(t.Context()); err == nil {
			t.Errorf("%s: SystemdVersion = %q", name, got)
		}
	}
}
