package declare

import (
	"errors"
	"maps"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/duty"
)

// The seat duties of PROPOSAL-seat-kinds.md (owner decisions OD1-OD5): a team's `duties`, a sparse
// map of member handle to duty, decoded strictly, gated with the security fields (OD3), and
// carried in team.json and approved.json.

const (
	podman2 = "myrepo.2+desk.podman"
	host3   = "myrepo.3+desk.host"
)

func decodeTeam0(t *testing.T, edit func(map[string]any)) (Team, error) {
	t.Helper()
	doc := validDecl()
	edit(team0(doc))
	d, err := Decode(encode(t, doc))
	if err != nil {
		return Team{}, err
	}
	return d.Teams[0], nil
}

func TestDutiesDecodes(t *testing.T) {
	tm, err := decodeTeam0(t, func(m map[string]any) {
		m["duties"] = map[string]any{podman2: "host", host3: "worker"}
	})
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	want := map[string]duty.Duty{podman2: duty.Host, host3: duty.Worker}
	if !maps.Equal(tm.Duties, want) {
		t.Fatalf("duties %v, want %v", tm.Duties, want)
	}
}

func TestDutiesAbsentIsNone(t *testing.T) {
	tm, err := decodeTeam0(t, func(map[string]any) {})
	if err != nil || tm.Duties != nil {
		t.Fatalf("duties %v, %v", tm.Duties, err)
	}
}

func TestDutyOfDerivesAndOverrides(t *testing.T) {
	tm, err := decodeTeam0(t, func(m map[string]any) {
		m["duties"] = map[string]any{podman2: "host"}
	})
	if err != nil {
		t.Fatal(err)
	}
	cases := map[string]duty.Duty{
		"myrepo.1+desk.podman": duty.Coordinator, // the coordinator
		podman2:                duty.Host,        // declared
		host3:                  duty.Host,        // a host member
		"myrepo.4+desk.lxc":    duty.Worker,      // every other member
	}
	for h, want := range cases {
		if got := tm.DutyOf(h); got != want {
			t.Errorf("DutyOf(%s) = %s, want %s", h, got, want)
		}
	}
}

func TestDutiesRefused(t *testing.T) {
	cases := map[string]struct {
		edit func(map[string]any)
		want string
	}{
		"unknown duty":  {func(m map[string]any) { m["duties"] = map[string]any{podman2: "reviewer"} }, "is not a duty"},
		"not a string":  {func(m map[string]any) { m["duties"] = map[string]any{podman2: 1} }, "is not a string"},
		"not an object": {func(m map[string]any) { m["duties"] = []any{podman2} }, "is not an object"},
		"a human":       {func(m map[string]any) { m["duties"] = map[string]any{"alice": "worker"} }, "is not an agent handle"},
		"the probe": {
			func(m map[string]any) { m["duties"] = map[string]any{"teamchat.probe+desk.host": "host"} },
			"is not an agent handle",
		},
		"the coordinator named": {
			func(m map[string]any) { m["duties"] = map[string]any{"myrepo.1+desk.podman": "worker"} },
			"is the team's coordinator",
		},
		"coordinator duty elsewhere": {
			func(m map[string]any) { m["duties"] = map[string]any{podman2: "coordinator"} },
			"only the team's coordinator",
		},
		"host member named coordinator": {
			func(m map[string]any) { m["duties"] = map[string]any{host3: "coordinator"} },
			"is a host member, which is never the coordinator",
		},
		"host member as the coordinator": {
			func(m map[string]any) { m["coordinator"] = host3 },
			"is a host member, which is never the coordinator",
		},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := decodeTeam0(t, c.edit)
			var fe *FieldError
			if !errors.As(err, &fe) || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("err %v, want a field error with %q", err, c.want)
			}
		})
	}
}

func TestDutiesCapped(t *testing.T) {
	_, err := decodeTeam0(t, func(m map[string]any) {
		many := map[string]any{}
		for i := range maxDuties + 1 {
			many["r"+strconv.Itoa(i)+".2+desk.podman"] = "worker"
		}
		m["duties"] = many
	})
	if err == nil || !strings.Contains(err.Error(), "holds more than") {
		t.Fatalf("err %v", err)
	}
}

func TestDutiesInTheTeamFileAndInput(t *testing.T) {
	doc := inputDoc()
	docTeam(doc)["duties"] = map[string]any{podman2: "host"}
	in := readInput(t, doc)
	raw := in.Team.Canonical()
	if !strings.Contains(string(raw), `"duties":{"`+podman2+`":"host"}`) {
		t.Fatalf("team.json %s", raw)
	}
	back, err := DecodeTeamFile(raw)
	if err != nil || !maps.Equal(back.Duties, in.Team.Duties) {
		t.Fatalf("team.json round trip %v, %v", back.Duties, err)
	}
	again, err := DecodeInput(in.Encode())
	if err != nil || !maps.Equal(again.Team.Duties, in.Team.Duties) {
		t.Fatalf("input round trip %v, %v", again.Team.Duties, err)
	}
	none := readInput(t, inputDoc()).Team.Canonical()
	if strings.Contains(string(none), `"duties"`) {
		t.Fatalf("a team with no duties writes the key: %s", none)
	}
}

func TestDutiesAreGated(t *testing.T) {
	approved := approvedOf(t, inputDoc())
	doc := inputDoc()
	docTeam(doc)["duties"] = map[string]any{podman2: "host"}
	got := Compare(approved, readInput(t, doc))
	want := []Difference{{Field: "duties", Old: none, New: podman2 + "=host"}}
	if !slices.Equal(got, want) {
		t.Fatalf("Compare = %v, want %v", got, want)
	}
	if err := Hold(approved, readInput(t, doc)); err == nil {
		t.Fatal("Hold passed an unapproved duty")
	}
	changed := approvedOf(t, doc)
	docTeam(doc)["duties"] = map[string]any{podman2: "worker"}
	got = Compare(changed, readInput(t, doc))
	want = []Difference{{Field: "duties", Old: podman2 + "=host", New: none}, {Field: "duties", Old: none, New: podman2 + "=worker"}}
	if !slices.Equal(got, want) {
		t.Fatalf("Compare a changed duty = %v, want %v", got, want)
	}
}

func TestApprovedFileCarriesDuties(t *testing.T) {
	alpha := mustTeam("alpha")
	doc := inputDoc()
	docTeam(doc)["duties"] = map[string]any{podman2: "host"}
	g := readInput(t, doc).Gated()
	raw := EncodeApproved(alpha, g)
	if !strings.Contains(string(raw), `"duties":{"`+podman2+`":"host"}`) {
		t.Fatalf("approved.json %s", raw)
	}
	back, err := DecodeApproved(raw, alpha)
	if err != nil || len(diff(g, back)) != 0 {
		t.Fatalf("round trip %v, %v", back.Duties, err)
	}
	empty := string(EncodeApproved(alpha, readInput(t, inputDoc()).Gated()))
	// A team with no duties writes no key, so its file is the one written before duties existed,
	// and such a file approves none.
	if strings.Contains(empty, `"duties"`) {
		t.Fatalf("approved.json with no duties %s", empty)
	}
	if g, err := DecodeApproved([]byte(empty), alpha); err != nil || g.Duties != nil {
		t.Fatalf("an approved.json without duties: %v, %v", g.Duties, err)
	}
	withEmpty := strings.Replace(empty, `"networks"`, `"duties":{},"networks"`, 1)
	if g, err := DecodeApproved([]byte(withEmpty), alpha); err != nil || g.Duties != nil {
		t.Fatalf("an approved.json with empty duties: %v, %v", g.Duties, err)
	}
	bad := strings.Replace(string(raw), `":"host"}`, `":"reviewer"}`, 1)
	if _, err := DecodeApproved([]byte(bad), alpha); err == nil {
		t.Fatal("an unknown duty in approved.json was accepted")
	}
}

func TestApproveOneDuty(t *testing.T) {
	approved := approvedOf(t, inputDoc())
	doc := inputDoc()
	docTeam(doc)["duties"] = map[string]any{podman2: "host"}
	in := readInput(t, doc)
	g, err := Approve(approved, in, &Change{Field: "duties", Op: OpAdd, Value: podman2 + "=host"})
	if err != nil || g.Duties[podman2] != duty.Host {
		t.Fatalf("approve --add: %v, %v", g.Duties, err)
	}
	for _, bad := range []Change{
		{Field: "duties", Op: OpAdd, Value: podman2 + "=worker"},
		{Field: "duties", Op: OpAdd, Value: podman2},
		{Field: "duties", Op: OpAdd, Value: podman2 + "=reviewer"},
		{Field: "duties", Op: OpAdd, Value: host3 + "=coordinator"},
		{Field: "duties", Op: OpRemove, Value: podman2 + "=host"},
		{Field: "duties", Op: OpSet, Value: podman2 + "=host"},
	} {
		if _, err := Approve(approved, in, &bad); err == nil {
			t.Errorf("approve %+v accepted", bad)
		}
	}
	docTeam(doc)["duties"] = map[string]any{podman2: "worker"}
	if _, err := Approve(&g, readInput(t, doc), &Change{Field: "duties", Op: OpSet, Value: podman2 + "=worker"}); err != nil {
		t.Fatalf("approve --set: %v", err)
	}
	delete(docTeam(doc), "duties")
	if _, err := Approve(&g, readInput(t, doc), &Change{Field: "duties", Op: OpRemove, Value: podman2 + "=host"}); err != nil {
		t.Fatalf("approve --remove: %v", err)
	}
}
