package duty

import "testing"

func TestParseIsTheClosedSet(t *testing.T) {
	for _, s := range []string{"coordinator", "worker", "host"} {
		if d, ok := Parse(s); !ok || string(d) != s {
			t.Errorf("Parse(%q) = %q, %v", s, d, ok)
		}
	}
	for _, s := range []string{"", "reviewer", "observer", "Worker", "probe", " host"} {
		if _, ok := Parse(s); ok {
			t.Errorf("Parse(%q) accepted", s)
		}
	}
}

func TestHostHandleIsTheHostType(t *testing.T) {
	cases := map[string]bool{
		"desk.1+home.host":   true,
		"desk.1+home.podman": false,
		"desk.1+home.lxc":    false,
		"not a handle":       false,
		"":                   false,
	}
	for h, want := range cases {
		if got := HostHandle(h); got != want {
			t.Errorf("HostHandle(%q) = %v, want %v", h, got, want)
		}
	}
}

func TestOfDerivesTheDefaults(t *testing.T) {
	const coord = "repo.1+home.podman"
	cases := []struct {
		handle string
		want   Duty
	}{
		{coord, Coordinator},
		{"repo.2+home.podman", Worker},
		{"repo.3+home.lxc", Worker},
		{"repo.4+home.host", Host},
	}
	for _, c := range cases {
		if got := Of(c.handle, coord, nil); got != c.want {
			t.Errorf("Of(%q) = %q, want %q", c.handle, got, c.want)
		}
	}
}

func TestOfTakesAnExplicitEntryOverTheDefault(t *testing.T) {
	explicit := map[string]Duty{"repo.2+home.podman": Host, "repo.4+home.host": Worker}
	if got := Of("repo.2+home.podman", "", explicit); got != Host {
		t.Errorf("podman member declared host: %q", got)
	}
	if got := Of("repo.4+home.host", "", explicit); got != Worker {
		t.Errorf("host member declared worker: %q", got)
	}
	if got := Of("repo.5+home.podman", "", explicit); got != Worker {
		t.Errorf("an undeclared member: %q", got)
	}
}
