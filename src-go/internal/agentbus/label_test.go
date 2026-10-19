package agentbus

import "testing"

func TestLabelIsTheWholeHandleWithTheCoordinatorMarked(t *testing.T) {
	cases := []struct{ handle, role, want string }{
		{"proj.1+local.podman", RoleCoordinator, "proj.1+local.podman (coordinator)"},
		{"proj.1+local.podman", RoleWorker, "proj.1+local.podman"},
		{"other.1+desk.podman", "", "other.1+desk.podman"},
		{"a-very-long-repository-name.12+build-host.lxc", RoleCoordinator, "a-very-long-repository-name.12+build-host.lxc (coordinator)"},
	}
	for _, c := range cases {
		if got := Label(c.handle, c.role); got != c.want {
			t.Errorf("Label(%q, %q) = %q, want %q", c.handle, c.role, got, c.want)
		}
	}
}

func TestAMembersLabelIsItsHandlesLabel(t *testing.T) {
	m := Member{Handle: "proj.2+local.podman", Role: RoleCoordinator}
	if m.Label() != Label(m.Handle, m.Role) {
		t.Fatalf("got %q", m.Label())
	}
}
