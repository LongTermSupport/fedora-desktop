package declare

import (
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/proto"
)

// teamFileVersion is the `v` of `team.json` and of `approved.json`.
const teamFileVersion = 1

// Input is one present team's declaration as `team apply` and `team approve` read it on stdin
// (DR7): the team, its LXC entries, and the bus address its default URL came from.
type Input struct {
	Team    Team
	LXC     []LXCMember
	Address string
}

// Inputs are the declaration's present teams, each with its own LXC entries, in declaration
// order.
func (d Declaration) Inputs() []Input {
	var out []Input
	for _, t := range d.Teams {
		if t.State != StatePresent {
			continue
		}
		in := Input{Team: t, Address: d.Address}
		for _, m := range d.LXC {
			if m.Team == t.ID {
				in.LXC = append(in.LXC, m)
			}
		}
		out = append(out, in)
	}
	return out
}

// Encode is the input as JSON, every default written out.
func (in Input) Encode() []byte {
	lxc := make([]proto.Value, 0, len(in.LXC))
	for _, m := range in.LXC {
		lxc = append(lxc, m.encode())
	}
	members := []proto.Member{
		proto.M(keyTeam, proto.Obj(append([]proto.Member{proto.M(keyState, proto.Str(string(StatePresent)))}, in.Team.members()...)...)),
		proto.M(keyLXCMembers, proto.Arr(lxc...)),
	}
	if in.Address != "" {
		members = append(members, proto.M(keyAddress, proto.Str(in.Address)))
	}
	return proto.Obj(members...).Compact()
}

func (m LXCMember) encode() proto.Value {
	members := []proto.Member{
		proto.M(keyTeam, proto.Str(m.Team.String())), proto.M(keyContainer, proto.Str(m.Container)),
		proto.M(keyUser, proto.Str(m.User.String())), proto.M(keyCheckout, proto.Str(m.Checkout)),
	}
	if m.Site != "" {
		members = append(members, proto.M(keySite, proto.Str(m.Site)))
	}
	if m.Seat != 0 {
		members = append(members, proto.M(keySeat, proto.Int(int64(m.Seat))))
	}
	return proto.Obj(append(members,
		proto.M(keyHumanText, proto.Bool(m.HumanText)), proto.M(keyService, proto.Bool(m.Service)),
		proto.M(keyState, proto.Str(string(m.State))))...)
}

// DecodeInput reads one team's input strictly (DR7): a present team, and LXC entries of that
// team only.
func DecodeInput(raw []byte) (Input, error) {
	v, err := parse(raw)
	if err != nil {
		return Input{}, err
	}
	f, err := node{v: v}.object([]string{keyTeam}, keyLXCMembers, keyAddress)
	if err != nil {
		return Input{}, err
	}
	var in Input
	if in.Address, err = f.optStr(keyAddress, ""); err != nil {
		return Input{}, err
	}
	if in.Address != "" && !validAddress(in.Address) {
		return Input{}, f.at(keyAddress).fail("is not an IP address in canonical form, or is unspecified")
	}
	if in.Team, err = readTeam(f.at(keyTeam), in.Address); err != nil {
		return Input{}, err
	}
	if in.Team.State != StatePresent {
		return Input{}, f.at(keyTeam).key(keyState).fail("must be present")
	}
	in.LXC, err = readLXC(f, keyLXCMembers, map[string]bool{in.Team.ID.String(): true})
	return in, err
}

// members is a present team's keys after `team` and before `state`'s place, defaults filled,
// in the canonical order (DR8).
func (t Team) members() []proto.Member {
	members := []proto.Member{
		proto.M(keyTeam, proto.Str(t.ID.String())), proto.M(keyName, proto.Str(t.Name)),
		proto.M(keyServerName, proto.Str(t.ServerName)), proto.M(keyPort, proto.Int(int64(t.Port))),
		proto.M(keyDescription, proto.Str(t.Description)), proto.M(keyListen, strArray(t.Listen)),
		proto.M(keyAllowFrom, strArray(t.AllowFrom)), proto.M(keyURL, proto.Str(t.URL)),
		proto.M(keyHumans, strArray(t.Humans)),
	}
	if t.Coordinator != "" {
		members = append(members, proto.M(keyCoordinator, proto.Str(t.Coordinator)))
	}
	return append(members, proto.M(keyTrust, t.Trust.Encode()))
}

// Canonical is the team's `team.json` (DR8).
func (t Team) Canonical() []byte {
	return proto.Obj(append([]proto.Member{proto.M(keyVersion, proto.Int(teamFileVersion))}, t.members()...)...).Compact()
}

var teamFileKeys = []string{
	keyVersion, keyTeam, keyName, keyServerName, keyPort, keyDescription, keyListen, keyAllowFrom,
	keyURL, keyHumans, keyTrust,
}

// DecodeTeamFile reads a `team.json` strictly (DR8).
func DecodeTeamFile(raw []byte) (Team, error) {
	v, err := parse(raw)
	if err != nil {
		return Team{}, err
	}
	f, err := node{path: "team.json", v: v}.object(teamFileKeys, keyCoordinator)
	if err != nil {
		return Team{}, err
	}
	if ver, verErr := f.at(keyVersion).integer(); verErr != nil || ver != teamFileVersion {
		return Team{}, f.at(keyVersion).fail("is not 1")
	}
	t := Team{State: StatePresent}
	if t.ID, err = teamID(f.at(keyTeam)); err != nil {
		return Team{}, err
	}
	return t, readValues(&t, f, "")
}

func strArray(list []string) proto.Value {
	items := make([]proto.Value, 0, len(list))
	for _, s := range list {
		items = append(items, proto.Str(s))
	}
	return proto.Arr(items...)
}

// ID is the input's team id.
func (in Input) ID() paths.TeamID { return in.Team.ID }
