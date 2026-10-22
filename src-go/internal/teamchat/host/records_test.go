package host

import (
	"net/netip"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
)

func sampleUnits(t *testing.T) unitsRecord {
	t.Helper()
	r, err := render(spec(t, input(t, teamDoc())))
	if err != nil {
		t.Fatal(err)
	}
	return unitsRecord{digests: r.digests, version: mustVersion(t, "1.9.4"), port: 8448, listen: []string{"192.0.2.10"}}
}

// HS5: units.json round-trips and is read strictly.
func TestUnitsRecordRoundTrip(t *testing.T) {
	team := mustTeam(t, "ops")
	u := sampleUnits(t)
	raw := u.encode(team)
	back, err := decodeUnits(raw, team)
	if err != nil {
		t.Fatalf("%v\n%s", err, raw)
	}
	if !reflect.DeepEqual(back, u) {
		t.Errorf("got %+v, want %+v", back, u)
	}
	if !strings.HasPrefix(string(raw), `{"v":1,"team":"ops","restart":{"base_unit":"`) {
		t.Errorf("shape %s", raw)
	}
}

func TestUnitsRecordRefusals(t *testing.T) {
	team := mustTeam(t, "ops")
	good := string(sampleUnits(t).encode(team))
	for name, raw := range map[string]string{
		"not json":       `{`,
		"not an object":  `[]`,
		"other team":     strings.Replace(good, `"team":"ops"`, `"team":"dev"`, 1),
		"bad team":       strings.Replace(good, `"team":"ops"`, `"team":"-"`, 1),
		"version 2":      strings.Replace(good, `"v":1`, `"v":2`, 1),
		"unknown key":    strings.Replace(good, `"v":1`, `"v":1,"x":1`, 1),
		"missing key":    strings.Replace(good, `"port":8448,`, ``, 1),
		"port a string":  strings.Replace(good, `"port":8448`, `"port":"8448"`, 1),
		"port 0":         strings.Replace(good, `"port":8448`, `"port":0`, 1),
		"listen not ip":  strings.Replace(good, `"192.0.2.10"`, `"x"`, 1),
		"listen string":  strings.Replace(good, `["192.0.2.10"]`, `"192.0.2.10"`, 1),
		"listen number":  strings.Replace(good, `["192.0.2.10"]`, `[1]`, 1),
		"bad version":    strings.Replace(good, `"tuwunel":"1.9.4"`, `"tuwunel":"x y"`, 1),
		"digest short":   strings.Replace(good, `"base_unit":"`, `"base_unit":"ab`, 1),
		"digest missing": strings.Replace(good, `"memory_high":`, `"memory_hi":`, 1),
		"digests array":  strings.Replace(good, `"reload":{`, `"reload":[{`, 1),
		"duplicate key":  strings.Replace(good, `"v":1`, `"v":1,"v":1`, 1),
	} {
		if _, err := decodeUnits([]byte(raw), team); err == nil {
			t.Errorf("%s: decoded %s", name, raw)
		}
	}
}

// HS7: firewall.json round-trips, in its order, and is read strictly.
func TestFirewallRecordRoundTrip(t *testing.T) {
	team := mustTeam(t, "ops")
	rules := []Rule{
		{Zone: "public", Source: netip.MustParsePrefix("192.0.2.0/24"), Port: 8448},
		{Zone: "trusted", Source: netip.MustParsePrefix("2001:db8::/32"), Port: 8448},
	}
	raw := encodeFirewall(team, rules)
	want := `{"v":1,"team":"ops","rules":[{"zone":"public","source":"192.0.2.0/24","port":8448},{"zone":"trusted","source":"2001:db8::/32","port":8448}]}`
	if string(raw) != want {
		t.Errorf("got %s", raw)
	}
	back, err := decodeFirewall(raw, team)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(back, rules) {
		t.Errorf("got %v", back)
	}
	empty, err := decodeFirewall(encodeFirewall(team, nil), team)
	if err != nil || len(empty) != 0 {
		t.Errorf("empty: %v %v", empty, err)
	}
	for name, raw := range map[string]string{
		"not json":       `{`,
		"other team":     strings.Replace(want, `"ops"`, `"dev"`, 1),
		"rules a map":    `{"v":1,"team":"ops","rules":{}}`,
		"rule a string":  `{"v":1,"team":"ops","rules":["x"]}`,
		"bad zone":       strings.Replace(want, `"public"`, `"pub lic"`, 1),
		"zone a number":  strings.Replace(want, `"public"`, `1`, 1),
		"unmasked":       strings.Replace(want, `"192.0.2.0/24"`, `"192.0.2.1/24"`, 1),
		"not a prefix":   strings.Replace(want, `"192.0.2.0/24"`, `"x"`, 1),
		"bad port":       strings.Replace(want, `"port":8448}`, `"port":70000}`, 1),
		"rule extra key": strings.Replace(want, `"port":8448}`, `"port":8448,"x":1}`, 1),
	} {
		if _, err := decodeFirewall([]byte(raw), team); err == nil {
			t.Errorf("%s: decoded %s", name, raw)
		}
	}
}

// HS7: a rule's rich rule names its family, source, port and protocol.
func TestRichRule(t *testing.T) {
	for r, want := range map[Rule]string{
		{Zone: "public", Source: netip.MustParsePrefix("192.0.2.0/24"), Port: 8448}:  `rule family="ipv4" source address="192.0.2.0/24" port port="8448" protocol="tcp" accept`,
		{Zone: "public", Source: netip.MustParsePrefix("2001:db8::/32"), Port: 9000}: `rule family="ipv6" source address="2001:db8::/32" port port="9000" protocol="tcp" accept`,
	} {
		if got := r.Rich(); got != want {
			t.Errorf("got %s, want %s", got, want)
		}
	}
	r := Rule{Zone: "public", Source: netip.MustParsePrefix("192.0.2.0/24"), Port: 8448}
	if got := r.String(); got != "public 192.0.2.0/24 tcp:8448" {
		t.Errorf("String %q", got)
	}
}

// HS11: the stamp is C 3.5.2's, and two stamps that differ only in applied_ms are the same.
func TestStamp(t *testing.T) {
	team := mustTeam(t, "ops")
	s := stamp{config: digest([]byte("{}")), software: "abc123", tuwunel: "1.9.4", appliedMS: 42}
	raw := s.encode(team)
	want := `{"v":1,"team":"ops","config":"` + digest([]byte("{}")) + `","software":"abc123","tuwunel":"1.9.4","applied_ms":42}`
	if string(raw) != want {
		t.Errorf("got %s", raw)
	}
	back, err := decodeStamp(raw, team)
	if err != nil {
		t.Fatal(err)
	}
	if back != s {
		t.Errorf("got %+v", back)
	}
	later := s
	later.appliedMS = 99
	if !later.sameAs(s) {
		t.Error("applied_ms makes a stamp differ")
	}
	later.software = "def"
	if later.sameAs(s) {
		t.Error("a new software digest is the same stamp")
	}
	for name, raw := range map[string]string{
		"not json":   `{`,
		"other team": strings.Replace(want, `"ops"`, `"dev"`, 1),
		"ms string":  strings.Replace(want, `42`, `"42"`, 1),
		"no config":  strings.Replace(want, `"config":`, `"conf":`, 1),
		"config int": `{"v":1,"team":"ops","config":1,"software":"abc123","tuwunel":"1.9.4","applied_ms":42}`,
	} {
		if _, err := decodeStamp([]byte(raw), team); err == nil {
			t.Errorf("%s: decoded", name)
		}
	}
}

// HR2: ReadStamp is a team's stamp under root, read by the trust rule; none is not found.
func TestReadStamp(t *testing.T) {
	team := mustTeam(t, "ops")
	root := t.TempDir()
	file := filepath.Join(root, paths.StampFile(team))
	if _, found, err := ReadStamp(root, team); found || err != nil {
		t.Fatalf("no stamp: %v, %v", found, err)
	}
	if err := os.MkdirAll(filepath.Dir(file), 0o750); err != nil {
		t.Fatal(err)
	}
	s := stamp{config: digest([]byte("{}")), software: "abc123", tuwunel: "1.9.4", appliedMS: 42}
	if err := os.WriteFile(file, s.encode(team), 0o600); err != nil {
		t.Fatal(err)
	}
	got, found, err := ReadStamp(root, team)
	want := Stamp{Config: s.config, Software: "abc123", Tuwunel: "1.9.4", AppliedMS: 42}
	if !found || err != nil || got != want {
		t.Fatalf("ReadStamp = %+v, %v, %v", got, found, err)
	}
	if _, _, err := ReadStamp(root, mustTeam(t, "dev")); err != nil {
		t.Fatalf("another team: %v", err)
	}
}

// HR2: a stamp that does not decode, or is a link, is an error and not found.
func TestReadStampRefuses(t *testing.T) {
	team := mustTeam(t, "ops")
	root := t.TempDir()
	file := filepath.Join(root, paths.StampFile(team))
	if err := os.MkdirAll(filepath.Dir(file), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, []byte(`{`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, found, err := ReadStamp(root, team); found || err == nil {
		t.Fatalf("a broken stamp: %v, %v", found, err)
	}
	if err := os.Remove(file); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(root, "elsewhere"), file); err != nil {
		t.Fatal(err)
	}
	if _, found, err := ReadStamp(root, team); found || err == nil {
		t.Fatalf("a linked stamp: %v, %v", found, err)
	}
}
