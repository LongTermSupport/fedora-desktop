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
	got, err := ParseSeats([]byte(out))
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

func TestParseSeatsRejectsMalformedLines(t *testing.T) {
	for _, line := range []string{
		"SEAT\talpha\t1\theld\tself",
		"SEAT\talpha\t1\tbusy\tself\th",
		"SEAT\talpha\t1\theld\tmaybe\th",
	} {
		if _, err := ParseSeats([]byte(line + "\n")); err == nil {
			t.Fatalf("accepted %q", line)
		}
	}
}

func TestParseRoster(t *testing.T) {
	out := "HUMAN\towner\tjoin\nHUMAN\tguest\t-\n" +
		"MEMBER\tproj.1+box.podman\tworker\tjoin\tactive\n" +
		"MEMBER\tproj.x+box.lxc\torchestrator\t-\tparked\n"
	got, err := ParseRoster([]byte(out))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got.Humans, []Human{{Name: "owner", Membership: "join"}, {Name: "guest"}}) {
		t.Fatalf("humans %+v", got.Humans)
	}
	wantMembers := []Member{
		{Handle: "proj.1+box.podman", Role: "worker", Membership: "join"},
		{Handle: "proj.x+box.lxc", Role: "orchestrator", Parked: true},
	}
	if !reflect.DeepEqual(got.Members, wantMembers) {
		t.Fatalf("members %+v", got.Members)
	}
}

func TestParseRosterRejectsMalformedLines(t *testing.T) {
	for _, line := range []string{
		"HUMAN\towner",
		"MEMBER\th\tworker\tjoin",
		"MEMBER\th\tworker\tjoin\tasleep",
	} {
		if _, err := ParseRoster([]byte(line + "\n")); err == nil {
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
	got, err := ParseDevices([]byte("DEVICE\tD1\t1700\nDEVICE\tD2\t-\nother\n"))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, []Device{{ID: "D1", LastSeenMs: "1700"}, {ID: "D2"}}) {
		t.Fatalf("got %+v", got)
	}
	if _, err := ParseDevices([]byte("DEVICE\tD1\n")); err == nil {
		t.Fatal("accepted a short DEVICE line")
	}
}

func TestParseBackups(t *testing.T) {
	got, err := ParseBackups([]byte("BACKUP\t2\t2026-01-02T03:04:05Z\nBACKUP\t1\tearlier\n"))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, []Backup{{ID: "2", Time: "2026-01-02T03:04:05Z"}, {ID: "1", Time: "earlier"}}) {
		t.Fatalf("got %+v", got)
	}
	if _, err := ParseBackups([]byte("BACKUP\t1\n")); err == nil {
		t.Fatal("accepted a short BACKUP line")
	}
}
