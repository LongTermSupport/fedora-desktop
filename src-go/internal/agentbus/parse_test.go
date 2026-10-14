package agentbus

import (
	"reflect"
	"testing"
)

func TestParseSeats(t *testing.T) {
	out := "SEAT\talpha\t1\theld\tself\tproj.1+box.podman\n" +
		"\n" +
		"SEAT\talpha\tqa\tfree\t-\tproj.qa+box.podman\n" +
		"CHANGED\tignored\n"
	got, err := ParseSeats(out)
	if err != nil {
		t.Fatal(err)
	}
	want := []Seat{
		{Team: "alpha", Seat: "1", Held: true, Self: true, Handle: "proj.1+box.podman"},
		{Team: "alpha", Seat: "qa", Held: false, Self: false, Handle: "proj.qa+box.podman"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v", got)
	}
}

func TestParseChoices(t *testing.T) {
	out := "CHOICE\t1\tfree\tproj.1+box.podman\ta ccy session\n" +
		"CHOICE\t2\tnew\tproj.2+box.podman\ta ccy session\n" +
		"DEFAULT\t1\n"
	got, err := ParseChoices(out)
	if err != nil {
		t.Fatal(err)
	}
	want := Choices{
		Rows: []Choice{
			{Seat: "1", State: "free", Handle: "proj.1+box.podman", Words: "a ccy session"},
			{Seat: "2", State: "new", Handle: "proj.2+box.podman", Words: "a ccy session"},
		},
		Default: "1",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v", got)
	}
}

func TestParseChoicesRejectsWhatIsNotUsable(t *testing.T) {
	for _, out := range []string{
		"",
		"DEFAULT\t1\n",
		"CHOICE\t1\tfree\th\tw\n",
		"CHOICE\t1\tfree\th\tw\nDEFAULT\t9\n",
		"CHOICE\t1\tfree\th\tw\nDEFAULT\t1\nCHANGED\tsurprise\n",
		"CHOICE\t1\tfree\th\nDEFAULT\t1\n",
	} {
		if _, err := ParseChoices(out); err == nil {
			t.Fatalf("accepted %q", out)
		}
	}
}

func TestParseSeatsRejectsMalformedLines(t *testing.T) {
	for _, line := range []string{
		"SEAT\talpha\t1\theld\tself",
		"SEAT\talpha\t1\tbusy\tself\th",
		"SEAT\talpha\t1\theld\tmaybe\th",
	} {
		if _, err := ParseSeats(line + "\n"); err == nil {
			t.Fatalf("accepted %q", line)
		}
	}
}

func TestParseRoster(t *testing.T) {
	out := "HUMAN\towner\tjoin\nHUMAN\tguest\t-\n" +
		"MEMBER\tproj.1+box.podman\tworker\tjoin\tactive\n" +
		"MEMBER\tproj.x+box.lxc\torchestrator\t-\tparked\n"
	got, err := ParseRoster(out)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got.Humans, []Human{{Name: "owner", Membership: "join"}, {Name: "guest"}}) {
		t.Fatalf("humans %+v", got.Humans)
	}
	wantMembers := []Member{
		{Handle: "proj.1+box.podman", Role: "worker", Membership: "join"},
		{Handle: "proj.x+box.lxc", Role: "coordinator", Parked: true},
	}
	if !reflect.DeepEqual(got.Members, wantMembers) {
		t.Fatalf("members %+v", got.Members)
	}
}

func TestParseRosterReadsTheOldRoleNameAsCoordinator(t *testing.T) {
	got, err := ParseRoster("MEMBER\tproj.x+box.lxc\torchestrator\t-\tactive\nMEMBER\tproj.y+box.lxc\tcoordinator\t-\tactive\n")
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range got.Members {
		if m.Role != RoleCoordinator {
			t.Fatalf("%s is %q, want %q", m.Handle, m.Role, RoleCoordinator)
		}
	}
}

func TestRosterCoordinator(t *testing.T) {
	roster := Roster{Members: []Member{
		{Handle: "a.1+h.podman", Role: RoleWorker},
		{Handle: "a.2+h.podman", Role: RoleCoordinator, Parked: true},
	}}
	holder, ok := roster.Coordinator()
	if !ok || holder.Handle != "a.2+h.podman" {
		t.Fatalf("got %+v %v", holder, ok)
	}
	if _, ok := (Roster{Members: roster.Members[:1]}).Coordinator(); ok {
		t.Fatal("a roster of workers has no coordinator")
	}
}

func TestRosterMemberFindsAHandle(t *testing.T) {
	roster := Roster{Members: []Member{{Handle: "a.1+h.podman", Role: RoleWorker}}}
	if m, ok := roster.Member("a.1+h.podman"); !ok || m.Role != RoleWorker {
		t.Fatalf("got %+v %v", m, ok)
	}
	if _, ok := roster.Member("a.2+h.podman"); ok {
		t.Fatal("found a member that is not there")
	}
}

func TestParseRosterRejectsMalformedLines(t *testing.T) {
	for _, line := range []string{
		"HUMAN\towner",
		"MEMBER\th\tworker\tjoin",
		"MEMBER\th\tworker\tjoin\tasleep",
	} {
		if _, err := ParseRoster(line + "\n"); err == nil {
			t.Fatalf("accepted %q", line)
		}
	}
}

func TestMemberKind(t *testing.T) {
	cases := map[string]string{
		"proj.1+box.podman": "ccy",
		"proj.x+box.lxc":    "lxc",
		"nodots":            "nodots",
	}
	for handle, want := range cases {
		if got := (Member{Handle: handle}).Kind(); got != want {
			t.Fatalf("%s: %q", handle, got)
		}
	}
}

func TestParseDevices(t *testing.T) {
	got, err := ParseDevices("DEVICE\tD1\t1700\nDEVICE\tD2\t-\nother\n")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, []Device{{ID: "D1", LastSeenMs: "1700"}, {ID: "D2"}}) {
		t.Fatalf("got %+v", got)
	}
	if _, err := ParseDevices("DEVICE\tD1\n"); err == nil {
		t.Fatal("accepted a short DEVICE line")
	}
}

func TestParseBackups(t *testing.T) {
	got, err := ParseBackups("BACKUP\t2\t2026-01-02T03:04:05Z\nBACKUP\t1\tearlier\n")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, []Backup{{ID: "2", Time: "2026-01-02T03:04:05Z"}, {ID: "1", Time: "earlier"}}) {
		t.Fatalf("got %+v", got)
	}
	if _, err := ParseBackups("BACKUP\t1\n"); err == nil {
		t.Fatal("accepted a short BACKUP line")
	}
}
