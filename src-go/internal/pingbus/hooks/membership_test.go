package hooks

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const (
	handleA = "proj.1+box.podman"
	handleB = "proj.2+box.podman"
)

func desiredJSON(seats, farewell string) string {
	return `{"v": 1, "session": "s1", "kind": "cc", "checkout": "/src/proj", "generation": 3, "written_ms": 5,` +
		` "seats": [` + seats + `], "farewell": [` + farewell + `]}`
}

func appliedJSON(seats string) string {
	return `{"v": 1, "generation": 3, "seats": [` + seats + `], "refused": []}`
}

func heldSeat(team, seat, handle string) string {
	return `{"team": "` + team + `", "seat": "` + seat + `", "handle": "` + handle + `", "state": "held"}`
}

// H17, membership.py parse_desired, parse_applied and _read_json: every key checked, duplicate
// keys refused, no symlink, at most 65536 bytes.
func TestReadDesiredAndApplied(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, desiredFile), desiredJSON(`{"team": "alpha", "seat": "1"}, {"team": "beta", "seat": "2"}`,
		`{"team": "gamma", "seat": "1", "reason": "removed"}`), 0o600)
	desired, err := readDesired(dir)
	if err != nil {
		t.Fatal(err)
	}
	if desired.session != "s1" || desired.kind != "cc" || desired.checkout != "/src/proj" || numberRepr(desired.generation) != "3" ||
		len(desired.seats) != 2 || desired.seats[1] != (seatEntry{"beta", "2"}) || desired.farewell[0] != (farewell{"gamma", "1", "removed"}) {
		t.Errorf("desired: %+v", desired)
	}
	home := t.TempDir()
	if applied, err := readApplied(home); err != nil || applied != nil {
		t.Errorf("no applied file: %+v %v", applied, err)
	}
	writeFile(t, filepath.Join(home, appliedFile), appliedJSON(heldSeat("beta", "2", handleB)+", "+heldSeat("alpha", "9", handleA)), 0o600)
	applied, err := readApplied(home)
	if err != nil || len(applied.seats) != 2 || applied.seats[0].handle != handleB {
		t.Fatalf("applied: %+v %v", applied, err)
	}
	if got := activeOf(desired, applied); strings.Join(got, ",") != "beta" {
		t.Errorf("active: %v (alpha's seat differs)", got)
	}
	if got := activeOf(desired, nil); len(got) != 0 {
		t.Errorf("active without applied: %v", got)
	}
}

func TestMembershipRefusals(t *testing.T) {
	good := desiredJSON(`{"team": "alpha", "seat": "1"}`, "")
	cases := map[string]string{
		"duplicate key":   strings.Replace(good, `"v": 1,`, `"v": 1, "v": 1,`, 1),
		"not an object":   `[]`,
		"missing key":     strings.Replace(good, `"kind": "cc", `, "", 1),
		"unknown key":     strings.Replace(good, `"v": 1,`, `"v": 1, "x": 1,`, 1),
		"v float":         strings.Replace(good, `"v": 1,`, `"v": 1.0,`, 1),
		"v true":          strings.Replace(good, `"v": 1,`, `"v": true,`, 1),
		"session":         strings.Replace(good, `"s1"`, `".s"`, 1),
		"kind":            strings.Replace(good, `"cc"`, `"vm"`, 1),
		"checkout":        strings.Replace(good, `"/src/proj"`, `"src"`, 1),
		"checkout ctl":    strings.Replace(good, `"/src/proj"`, `"/a\u0007"`, 1),
		"generation 0":    strings.Replace(good, `"generation": 3`, `"generation": 0`, 1),
		"written_ms":      strings.Replace(good, `"written_ms": 5`, `"written_ms": -1`, 1),
		"seat name":       strings.Replace(good, `"seat": "1"`, `"seat": "01"`, 1),
		"two seats":       desiredJSON(`{"team": "alpha", "seat": "1"}, {"team": "alpha", "seat": "2"}`, ""),
		"seats not list":  strings.Replace(good, `"seats": [{"team": "alpha", "seat": "1"}]`, `"seats": {}`, 1),
		"farewell reason": desiredJSON("", `{"team": "alpha", "seat": "1", "reason": "gone"}`),
		"not UTF-8":       strings.Replace(good, `"s1"`, "\"s\xff\"", 1),
		"too large":       good + strings.Repeat(" ", maxMembershipBytes),
	}
	for name, text := range cases {
		dir := t.TempDir()
		writeFile(t, filepath.Join(dir, desiredFile), text, 0o600)
		_, err := readDesired(dir)
		if _, ok := errors.AsType[*membershipError](err); !ok {
			t.Errorf("%s: %v, want a membership error", name, err)
		}
	}
	dir := t.TempDir()
	if _, err := readDesired(dir); err == nil || !strings.Contains(err.Error(), "no such file") {
		t.Errorf("absent desired: %v", err)
	}
	writeFile(t, filepath.Join(dir, "real.json"), good, 0o600)
	if err := os.Symlink("real.json", filepath.Join(dir, desiredFile)); err != nil {
		t.Fatal(err)
	}
	if _, err := readDesired(dir); err == nil {
		t.Error("a symlinked desired file must be refused")
	}
	// A farewell reason Python cannot hash is a TypeError: internal, not a refusal.
	unhashable := t.TempDir()
	writeFile(t, filepath.Join(unhashable, desiredFile), desiredJSON("", `{"team": "alpha", "seat": "1", "reason": []}`), 0o600)
	if _, err := readDesired(unhashable); !errors.As(err, new(*internalError)) {
		t.Errorf("unhashable reason: %v, want internal", err)
	}
	for name, text := range map[string]string{
		"state":   appliedJSON(strings.Replace(heldSeat("alpha", "1", handleA), "held", "free", 1)),
		"handle":  appliedJSON(heldSeat("alpha", "1", "nope")),
		"refused": `{"v": 1, "generation": 0, "seats": [], "refused": [{"team": "alpha", "seat": "1", "why": "x", "holder": "-"}]}`,
		"holder":  `{"v": 1, "generation": 0, "seats": [], "refused": [{"team": "alpha", "seat": "1", "why": "no-bundle", "holder": "A"}]}`,
		"inbound": `{"v": 1, "generation": 0, "seats": [], "refused": [], "inbound": 1}`,
		"told":    `{"v": 1, "generation": 0, "seats": [], "refused": [], "told": {"generation": 0, "roles": [{"team": "a", "handle": "x", "role": "worker"}]}}`,
	} {
		home := t.TempDir()
		writeFile(t, filepath.Join(home, appliedFile), text, 0o600)
		if _, err := readApplied(home); err == nil {
			t.Errorf("applied %s: accepted", name)
		}
	}
	home := t.TempDir()
	writeFile(t, filepath.Join(home, appliedFile), `{"v": 1, "generation": 0, "seats": [], "refused": [{"team": "alpha", "seat": "1", "why": "held-elsewhere", "holder": "other-host"}],`+
		` "told": {"generation": 1, "roles": [{"team": "alpha", "handle": "`+handleA+`", "role": "worker"}]}, "inbound": false}`, 0o600)
	if _, err := readApplied(home); err != nil {
		t.Errorf("a full applied file: %v", err)
	}
}

// H83: told.json and told-roles.json, read by the same rules and written sorted with a newline.
func TestToldRecords(t *testing.T) {
	home := t.TempDir()
	if record, err := readTold(home); err != nil || record != nil {
		t.Errorf("no told.json: %+v %v", record, err)
	}
	if roles, err := readToldRoles(home); err != nil || roles != nil {
		t.Errorf("no told-roles.json: %+v %v", roles, err)
	}
	record := toldRecord{generation: intNumber(4), teams: []toldTeam{{"alpha", "1", handleA, "-"}}, left: []toldLeft{{"beta", "removed"}}}
	if err := writeTold(home, record); err != nil {
		t.Fatal(err)
	}
	assertFile(t, filepath.Join(home, toldFile), `{"generation": 4, "left": [{"reason": "removed", "team": "beta"}], `+
		`"teams": [{"handle": "`+handleA+`", "role": "-", "seat": "1", "team": "alpha"}], "v": 1}`+"\n", 0o600)
	again, err := readTold(home)
	if err != nil || !again.equal(record) {
		t.Errorf("read back: %+v %v", again, err)
	}
	if err := writeToldRoles(home, map[string]string{"beta": "worker", "alpha": "coordinator"}); err != nil {
		t.Fatal(err)
	}
	assertFile(t, filepath.Join(home, toldRolesFile), `{"roles": {"alpha": "coordinator", "beta": "worker"}, "v": 1}`+"\n", 0o600)
	if roles, err := readToldRoles(home); err != nil || roles["alpha"] != "coordinator" || len(roles) != 2 {
		t.Errorf("roles: %v %v", roles, err)
	}
	for name, text := range map[string]string{
		"role":   `{"v": 1, "generation": 1, "teams": [{"team": "a", "seat": "1", "handle": "` + handleA + `", "role": "boss"}], "left": []}`,
		"reason": `{"v": 1, "generation": 1, "teams": [], "left": [{"team": "a", "reason": "x"}]}`,
		"gen":    `{"v": 1, "generation": -1, "teams": [], "left": []}`,
	} {
		writeFile(t, filepath.Join(home, toldFile), text, 0o600)
		if _, err := readTold(home); err == nil {
			t.Errorf("told %s: accepted", name)
		}
	}
	for name, text := range map[string]string{
		"role":   `{"v": 1, "roles": {"a": "boss"}}`,
		"team":   `{"v": 1, "roles": {"A": "worker"}}`,
		"object": `{"v": 1, "roles": []}`,
	} {
		writeFile(t, filepath.Join(home, toldRolesFile), text, 0o600)
		if _, err := readToldRoles(home); err == nil {
			t.Errorf("told-roles %s: accepted", name)
		}
	}
}
