package host

import (
	"errors"
	"fmt"
	"maps"
	"net/netip"
	"regexp"
	"slices"
	"strconv"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/proto"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/state"
)

// recordVersion is the `v` of units.json, firewall.json and the stamp.
const recordVersion = 1

var (
	digestGrammar = regexp.MustCompile(`^[0-9a-f]{64}$`)
	zoneGrammar   = regexp.MustCompile(`^[A-Za-z0-9_-]{1,17}$`)
)

var (
	restartKeys = []string{digestBaseUnit, digestConfig, digestExecStart}
	reloadKeys  = []string{digestIPAllow, digestMemory, digestSocketBind, digestUnit}
)

// unitsRecord is units.json (C 3.2.2): the digest sets, version, port and listen set last
// started.
type unitsRecord struct {
	digests digests
	version paths.ServerVersion
	port    int
	listen  []string
}

func (u unitsRecord) encode(team paths.TeamID) []byte {
	return proto.Obj(
		proto.M("v", proto.Int(recordVersion)), proto.M("team", proto.Str(team.String())),
		proto.M("restart", digestObject(u.digests.Restart)), proto.M("reload", digestObject(u.digests.Reload)),
		proto.M("tuwunel", proto.Str(u.version.String())), proto.M("port", proto.Int(int64(u.port))),
		proto.M("listen", strArray(u.listen)),
	).Compact()
}

func digestObject(set map[string]string) proto.Value {
	members := make([]proto.Member, 0, len(set))
	for _, k := range slices.Sorted(maps.Keys(set)) {
		members = append(members, proto.M(k, proto.Str(set[k])))
	}
	return proto.Obj(members...)
}

func strArray(list []string) proto.Value {
	items := make([]proto.Value, 0, len(list))
	for _, s := range list {
		items = append(items, proto.Str(s))
	}
	return proto.Arr(items...)
}

func decodeUnits(raw []byte, team paths.TeamID) (unitsRecord, error) {
	r, err := readRecord(raw, team, "units.json", "restart", "reload", "tuwunel", "port", "listen")
	if err != nil {
		return unitsRecord{}, err
	}
	u := unitsRecord{
		digests: digests{Restart: r.digestSet("restart", restartKeys), Reload: r.digestSet("reload", reloadKeys)},
		port:    r.port("port"),
	}
	if version, perr := paths.ParseServerVersion(r.str("tuwunel")); perr == nil {
		u.version = version
	} else {
		r.fail("tuwunel", "is not a server version")
	}
	for i, v := range r.array("listen") {
		if addr, perr := netip.ParseAddr(v.Str()); v.Kind() != proto.KindString || perr != nil || addr.String() != v.Str() {
			r.fail("listen["+strconv.Itoa(i)+"]", "is not an IP address in canonical form")
		}
		u.listen = append(u.listen, v.Str())
	}
	return u, r.err
}

// Rule is one firewalld rich rule a team added: the zone it is in, the network it admits, and
// the team's port then.
type Rule struct {
	Zone   string
	Source netip.Prefix
	Port   int
}

// Rich is the rule's firewalld rich rule text.
func (r Rule) Rich() string {
	family := "ipv4"
	if r.Source.Addr().Is6() {
		family = "ipv6"
	}
	return `rule family="` + family + `" source address="` + r.Source.String() + `" port port="` + strconv.Itoa(r.Port) + `" protocol="tcp" accept`
}

// String is the rule as a CHANGED line names it.
func (r Rule) String() string {
	return r.Zone + " " + r.Source.String() + " tcp:" + strconv.Itoa(r.Port)
}

func encodeFirewall(team paths.TeamID, rules []Rule) []byte {
	items := make([]proto.Value, 0, len(rules))
	for _, r := range rules {
		items = append(items, proto.Obj(
			proto.M("zone", proto.Str(r.Zone)), proto.M("source", proto.Str(r.Source.String())),
			proto.M("port", proto.Int(int64(r.Port))),
		))
	}
	return proto.Obj(proto.M("v", proto.Int(recordVersion)), proto.M("team", proto.Str(team.String())), proto.M("rules", proto.Arr(items...))).Compact()
}

func decodeFirewall(raw []byte, team paths.TeamID) ([]Rule, error) {
	r, err := readRecord(raw, team, "firewall.json", "rules")
	if err != nil {
		return nil, err
	}
	var rules []Rule
	for i, v := range r.array("rules") {
		at := "rules[" + strconv.Itoa(i) + "]"
		f := r.sub(at, v, "zone", "source", "port")
		if f == nil {
			continue
		}
		rule := Rule{Zone: f.str("zone"), Port: f.port("port")}
		if !zoneGrammar.MatchString(rule.Zone) {
			r.fail(at+".zone", "is not a firewalld zone name")
		}
		if p, perr := netip.ParsePrefix(f.str("source")); perr == nil && p.Masked() == p {
			rule.Source = p
		} else {
			r.fail(at+".source", "is not a network in canonical masked form")
		}
		rules = append(rules, rule)
	}
	return rules, r.err
}

// stamp is the team's install stamp (C 3.5.2): a stamp means fully applied.
type stamp struct {
	config, software, tuwunel string
	appliedMS                 int64
}

// sameAs is whether two stamps say the same apply, whenever each was written.
func (s stamp) sameAs(o stamp) bool {
	s.appliedMS = o.appliedMS
	return s == o
}

func (s stamp) encode(team paths.TeamID) []byte {
	return proto.Obj(
		proto.M("v", proto.Int(recordVersion)), proto.M("team", proto.Str(team.String())),
		proto.M("config", proto.Str(s.config)), proto.M("software", proto.Str(s.software)),
		proto.M("tuwunel", proto.Str(s.tuwunel)), proto.M("applied_ms", proto.Int(s.appliedMS)),
	).Compact()
}

func decodeStamp(raw []byte, team paths.TeamID) (stamp, error) {
	r, err := readRecord(raw, team, "stamp.json", "config", "software", "tuwunel", "applied_ms")
	if err != nil {
		return stamp{}, err
	}
	s := stamp{config: r.str("config"), software: r.str("software"), tuwunel: r.str("tuwunel")}
	if v := r.get("applied_ms"); v.Kind() == proto.KindInt {
		s.appliedMS = v.Int()
	} else {
		r.fail("applied_ms", "is not an integer")
	}
	return s, r.err
}

// Stamp is a team's install stamp as another package reads it (C 3.5.2): the config and
// software digests, the Tuwunel version, and when it was applied.
type Stamp struct {
	Config, Software, Tuwunel string
	AppliedMS                 int64
}

// ReadStamp is team's stamp under root ("" on a host), read by the trust rule
// (state.ReadFile) and decoded strictly; found is false when there is none.
func ReadStamp(root string, team paths.TeamID) (Stamp, bool, error) {
	raw, found, err := state.ReadFile(root+paths.StampFile(team), fileMax)
	if err != nil || !found {
		return Stamp{}, false, err
	}
	s, err := decodeStamp(raw, team)
	if err != nil {
		return Stamp{}, false, err
	}
	return Stamp{Config: s.config, Software: s.software, Tuwunel: s.tuwunel, AppliedMS: s.appliedMS}, true, nil
}

// record reads one object's fields strictly, keeping the first problem; a nested object's
// record hands its problems to its parent.
type record struct {
	name   string
	fields map[string]proto.Value
	parent *record
	err    error
}

// readRecord parses raw as an object with exactly `v`, `team` and keys, `v` 1 and `team` this
// team.
func readRecord(raw []byte, team paths.TeamID, name string, keys ...string) (*record, error) {
	v, err := proto.Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", name, err)
	}
	r := &record{name: name}
	r.fields = r.object(name, v, append([]string{"v", "team"}, keys...))
	if r.err != nil {
		return nil, r.err
	}
	if ver := r.get("v"); ver.Kind() != proto.KindInt || ver.Int() != recordVersion {
		return nil, fmt.Errorf("%s: v is not %d", name, recordVersion)
	}
	if got := r.str("team"); got != team.String() {
		return nil, fmt.Errorf("%s: team is not %s", name, team)
	}
	return r, r.err
}

// object is v's members when v is an object with exactly keys.
func (r *record) object(at string, v proto.Value, keys []string) map[string]proto.Value {
	if v.Kind() != proto.KindObject {
		r.fail(at, "is not an object")
		return nil
	}
	fields := map[string]proto.Value{}
	for _, m := range v.Members() {
		fields[m.Key] = m.Val
	}
	if got := slices.Sorted(maps.Keys(fields)); !slices.Equal(got, slices.Sorted(slices.Values(keys))) {
		r.fail(at, fmt.Sprintf("has the keys %q, not %q", got, keys))
		return nil
	}
	return fields
}

func (r *record) fail(at, problem string) {
	if r.parent != nil {
		r.parent.fail(r.name+"."+at, problem)
		return
	}
	if r.err == nil {
		r.err = errors.New(r.name + ": " + at + " " + problem)
	}
}

func (r *record) get(key string) proto.Value { return r.fields[key] }

func (r *record) str(key string) string {
	v := r.get(key)
	if v.Kind() != proto.KindString {
		r.fail(key, "is not a string")
	}
	return v.Str()
}

func (r *record) port(key string) int {
	v := r.get(key)
	if v.Kind() != proto.KindInt {
		r.fail(key, "is not an integer")
		return 0
	}
	p, err := paths.ParsePort(int(v.Int()))
	if err != nil {
		r.fail(key, "is not a port")
		return 0
	}
	return p.Int()
}

func (r *record) array(key string) []proto.Value {
	v := r.get(key)
	if v.Kind() != proto.KindArray {
		r.fail(key, "is not an array")
	}
	return v.Items()
}

// sub is a nested object with exactly keys, read by the same record, or nil.
func (r *record) sub(at string, v proto.Value, keys ...string) *record {
	fields := r.object(at, v, keys)
	if fields == nil {
		return nil
	}
	return &record{name: at, fields: fields, parent: r}
}

func (r *record) digestSet(key string, keys []string) map[string]string {
	f := r.sub(key, r.get(key), keys...)
	if f == nil {
		return nil
	}
	set := map[string]string{}
	for _, k := range keys {
		set[k] = f.str(k)
		if !digestGrammar.MatchString(set[k]) {
			f.fail(k, "is not a SHA-256 digest")
		}
	}
	return set
}
