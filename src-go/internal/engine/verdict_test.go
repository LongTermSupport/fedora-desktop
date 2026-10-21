package engine

import "testing"

// The cases of scripts/test-ccy-rootless-guard.bash.
func TestRootlessVerdict(t *testing.T) {
	cases := []struct {
		name   string
		kind   Kind
		report string
		want   Posture
	}{
		{"rootless daemon reports true", Podman, "true", PostureRootless},
		{"trailing newline is not a third state", Podman, "true\n", PostureRootless},
		{"leading and trailing spaces tolerated", Podman, "  true ", PostureRootless},
		{"rootful daemon reports false", Podman, "false", PostureRootful},
		{"docker rootless daemon", Docker, "[name=rootless name=seccomp,profile=builtin name=cgroupns]", PostureRootless},
		{"docker rootful daemon", Docker, "[name=seccomp,profile=builtin name=cgroupns]", PostureRootful},
		{"empty output", Podman, "", PostureUnknown},
		{"whitespace-only output", Podman, "  \n\t ", PostureUnknown},
		{"info failed, error in capture", Podman, "Error: unable to connect to Podman socket", PostureUnknown},
		{"field moved (Go <no value>)", Podman, "<no value>", PostureUnknown},
		{"unparseable value", Podman, "maybe", PostureUnknown},
		{"docker daemon unreachable", Docker, "Cannot connect to the Docker daemon.", PostureUnknown},
		{"docker empty", Docker, "", PostureUnknown},
		{"engine ccy does not know", Kind("nerdctl"), "true", PostureUnknown},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := RootlessVerdict(c.kind, c.report); got != c.want {
				t.Fatalf("RootlessVerdict(%q, %q) = %v, want %v", c.kind, c.report, got, c.want)
			}
		})
	}
}

// The suite's discrimination check: the two verdicts are reachable and distinct.
func TestRootlessVerdictDiscriminates(t *testing.T) {
	if RootlessVerdict(Podman, "true") == RootlessVerdict(Podman, "false") {
		t.Fatal("rootless and rootful verdicts are identical")
	}
}

// The cases of scripts/test-ccy-selinux-verdict.bash.
func TestSELinuxVerdict(t *testing.T) {
	cases := []struct {
		name            string
		enforce, report string
		want            SELinux
	}{
		{"Enforcing host, engine labelling", "Enforcing", "true", SELinuxEnforcing},
		{"trailing newline on the engine report is not a third state", "Enforcing", "true\n", SELinuxEnforcing},
		{"surrounding whitespace on both answers", " Enforcing ", " true ", SELinuxEnforcing},
		{"Enforcing host but the engine does not label", "Enforcing", "false", SELinuxOff},
		{"Permissive host, engine labelling", "Permissive", "true", SELinuxPermissive},
		{"Permissive host, engine not labelling", "Permissive", "false", SELinuxOff},
		{"Permissive host, engine report empty", "Permissive", "", SELinuxUnknown},
		{"Disabled host", "Disabled", "true", SELinuxOff},
		{"Disabled host, engine not labelling", "Disabled", "false", SELinuxOff},
		{"no getenforce and the engine agrees", "", "false", SELinuxOff},
		{"no getenforce but the engine claims labelling", "", "true", SELinuxUnknown},
		{"nothing readable from either", "", "", SELinuxUnknown},
		{"Enforcing host, engine report empty", "Enforcing", "", SELinuxUnknown},
		{"Enforcing host, engine error text captured as the report", "Enforcing", "Error: cannot connect", SELinuxUnknown},
		{"shell error text where getenforce output should be", "getenforce: command not found", "true", SELinuxUnknown},
		{"lower-case is not getenforce's spelling", "enforcing", "true", SELinuxUnknown},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := SELinuxVerdict(c.enforce, c.report); got != c.want {
				t.Fatalf("SELinuxVerdict(%q, %q) = %v, want %v", c.enforce, c.report, got, c.want)
			}
		})
	}
}

func TestSELinuxRelabelIsTheSharedOneUnlessOff(t *testing.T) {
	cases := map[SELinux]string{
		SELinuxEnforcing:  "z",
		SELinuxPermissive: "z",
		SELinuxUnknown:    "z",
		SELinuxOff:        "",
	}
	for mode, want := range cases {
		if got := mode.Relabel(); got != want {
			t.Errorf("%v.Relabel() = %q, want %q", mode, got, want)
		}
	}
}

func TestDockerSELinuxReport(t *testing.T) {
	cases := []struct {
		name, options, want string
	}{
		{"labelling daemon", "[name=seccomp,profile=builtin name=selinux]", "true"},
		{"daemon that does not label", "[name=seccomp,profile=builtin name=cgroupns]", "false"},
		// The bash launcher read any text without selinux in it as "false", so an
		// unreachable daemon turned the relabel off.
		{"unreachable daemon stays unreadable", "Cannot connect to the Docker daemon.", "Cannot connect to the Docker daemon."},
		{"empty stays empty", "", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := DockerSELinuxReport(c.options); got != c.want {
				t.Fatalf("DockerSELinuxReport(%q) = %q, want %q", c.options, got, c.want)
			}
		})
	}
}

func TestVerdictStrings(t *testing.T) {
	postures := map[Posture]string{PostureRootless: "rootless", PostureRootful: "rootful", PostureUnknown: "unknown"}
	for p, want := range postures {
		if p.String() != want {
			t.Errorf("%d.String() = %q, want %q", p, p.String(), want)
		}
	}
	modes := map[SELinux]string{
		SELinuxEnforcing: "enforcing", SELinuxPermissive: "permissive", SELinuxOff: "off", SELinuxUnknown: "unknown",
	}
	for m, want := range modes {
		if m.String() != want {
			t.Errorf("%d.String() = %q, want %q", m, m.String(), want)
		}
	}
}
