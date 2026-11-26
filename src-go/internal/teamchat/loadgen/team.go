package loadgen

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/clock"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/matrix"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/proto"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/rooms"
)

const (
	// TeamName is the test team's id and display name.
	TeamName = "loadgen"
	// TeamFile is the team file's name in its directory.
	TeamFile = "team.json"
	// TeamFileVersion is the team file's `v`.
	TeamFileVersion = 1
	// MaxAgents is the most agent accounts a team holds (the roster's 64 members, A 11.2).
	MaxAgents = 64
	// MinAgents is the fewest: a coordinator and a worker, so every agent has another to ping.
	MinAgents = 2
	// RoleHuman is the human account's role in the team file.
	RoleHuman = "human"
	// humanLocalpart is the human account's localpart.
	humanLocalpart = "tester"
	// forgeOrg is the roster's forge org, a reserved example name.
	forgeOrg = "example-org"
)

// Clock is the time the generator runs on: the wall clock's milliseconds and a sleep that
// ends with its context. clock.System is the real one.
type Clock interface {
	WallMS() int64
	Sleep(ctx context.Context, d time.Duration) error
}

// Account is one account of the test team: its user ID, its role and its access token.
type Account struct {
	UserID string `json:"user_id"`
	Role   string `json:"role"`
	Token  string `json:"token"`
}

// Rooms are the team's three rooms.
type Rooms struct {
	Main      string `json:"main"`
	Reports   string `json:"reports"`
	Questions string `json:"questions"`
}

// Team is the team file (LG2): where the lab server is, the rooms, every account's token and
// the human's password, for Lightning's sign-in.
type Team struct {
	V             int       `json:"v"`
	Homeserver    string    `json:"homeserver"`
	ServerName    string    `json:"server_name"`
	Team          string    `json:"team"`
	Rooms         Rooms     `json:"rooms"`
	Agents        []Account `json:"agents"`
	Human         Account   `json:"human"`
	HumanPassword string    `json:"human_password"`
}

// check is a team file's rule: this version, LG1's target, the rooms and at least one agent.
func (t Team) check() error {
	switch {
	case t.V != TeamFileVersion:
		return fmt.Errorf("the team file's version is %d, want %d", t.V, TeamFileVersion)
	case len(t.Agents) < MinAgents || len(t.Agents) > MaxAgents:
		return fmt.Errorf("the team file holds %d agents, want %d to %d", len(t.Agents), MinAgents, MaxAgents)
	case t.Rooms.Main == "" || t.Rooms.Reports == "" || t.Rooms.Questions == "":
		return errors.New("the team file does not name all three rooms")
	}
	return CheckTarget(t.Homeserver, t.ServerName)
}

// SetupOptions are a setup's target, the server's registration secret and the agents to make.
type SetupOptions struct {
	Base, ServerName, RegKey string
	Agents                   int
	Clock                    Clock
}

// agentID is agent i's user ID (from 1): a seat handle, as a real member's is.
func agentID(i int, server string) string {
	return proto.UserID(TeamName+"."+strconv.Itoa(i)+"+lab.podman", server)
}

func randomKey() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// Setup is LG2: the team's admin registered on a fresh lab server, its accounts made, its rooms
// converged and joined.
func Setup(ctx context.Context, o SetupOptions) (Team, error) {
	if o.Agents < MinAgents || o.Agents > MaxAgents {
		return Team{}, fmt.Errorf("--agents %d: give %d to %d (a coordinator and at least one worker to ping)", o.Agents, MinAgents, MaxAgents)
	}
	if err := CheckTarget(o.Base, o.ServerName); err != nil {
		return Team{}, err
	}
	if o.RegKey == "" {
		return Team{}, errors.New("the registration key file is empty: give the lab server's registration shared secret")
	}
	adm, err := registerAdmin(ctx, o)
	if err != nil {
		return Team{}, err
	}
	defer adm.Close()
	team := Team{V: TeamFileVersion, Homeserver: o.Base, ServerName: o.ServerName, Team: TeamName}
	if team.HumanPassword, err = randomKey(); err != nil {
		return Team{}, err
	}
	team.Human = Account{UserID: proto.UserID(humanLocalpart, o.ServerName), Role: RoleHuman}
	if err := makeAccount(ctx, adm, team.Human.UserID, team.HumanPassword); err != nil {
		return Team{}, err
	}
	for i := 1; i <= o.Agents; i++ {
		a := Account{UserID: agentID(i, o.ServerName), Role: proto.RoleWorker}
		if i == 1 {
			a.Role = proto.RoleCoordinator
		}
		password, err := randomKey()
		if err != nil {
			return Team{}, err
		}
		if err := makeAccount(ctx, adm, a.UserID, password); err != nil {
			return Team{}, err
		}
		team.Agents = append(team.Agents, a)
	}
	if team.Rooms, err = converge(ctx, adm, team); err != nil {
		return Team{}, err
	}
	if err := join(ctx, adm, &team, o.Clock); err != nil {
		return Team{}, err
	}
	return team, nil
}

// registerAdmin registers the team's admin with the registration secret and holds the
// server's answer to the name (LG1).
func registerAdmin(ctx context.Context, o SetupOptions) (*matrix.Admin, error) {
	anon, err := matrix.NewAdmin(matrix.Config{BaseURL: o.Base, Clock: o.Clock})
	if err != nil {
		return nil, err
	}
	defer anon.Close()
	password, err := randomKey()
	if err != nil {
		return nil, err
	}
	sess, err := anon.Register(ctx, o.RegKey, matrix.Registration{Username: proto.AdminLocalpart, Password: password, Admin: true})
	if err != nil {
		return nil, fmt.Errorf("register the team's admin: %w: a setup needs a fresh lab server and its registration secret", err)
	}
	if err := CheckServer(sess.UserID, o.ServerName); err != nil {
		return nil, err
	}
	return anon.WithToken(sess.AccessToken.Reveal())
}

func makeAccount(ctx context.Context, adm *matrix.Admin, userID, password string) error {
	if _, err := adm.CreateAccount(ctx, userID, password, ""); err != nil {
		return fmt.Errorf("make the account %s: %w", userID, err)
	}
	return nil
}

// roster is the team's roster as the host derives it: the agents' roles, the human, the forge
// section read as a play's would be, the default limits.
func roster(team Team) (proto.Roster, error) {
	forge, err := proto.DecodeForgeInput(proto.Obj(proto.M("kind", proto.Str(proto.ForgeGitHub)), proto.M("org", proto.Str(forgeOrg))))
	if err != nil {
		return proto.Roster{}, err
	}
	r := proto.Roster{
		Team: TeamName, Name: TeamName, Humans: []string{team.Human.UserID}, Members: map[string]proto.RosterMember{},
		Forge: forge, Limits: proto.DefaultLimits(),
	}
	for _, a := range team.Agents {
		r.Members[a.UserID] = proto.RosterMember{Role: a.Role, HumanText: true}
	}
	return r, nil
}

// recorder keeps the room IDs the engine records.
type recorder struct{ ids Rooms }

func (*recorder) Intent(string) error { return nil }

func (r *recorder) Record(purpose, id string) error {
	switch purpose {
	case proto.PurposeMain:
		r.ids.Main = id
	case proto.PurposeReports:
		r.ids.Reports = id
	case proto.PurposeQuestions:
		r.ids.Questions = id
	default:
		return fmt.Errorf("the rooms engine recorded a room %q this team does not have", purpose)
	}
	return nil
}

func converge(ctx context.Context, adm *matrix.Admin, team Team) (Rooms, error) {
	r, err := roster(team)
	if err != nil {
		return Rooms{}, err
	}
	rec := &recorder{}
	if _, err := rooms.Converge(ctx, adm, rec, rooms.Team{Server: team.ServerName, Team: TeamName, Roster: r}, proto.RoomIDs{}); err != nil {
		return Rooms{}, fmt.Errorf("converge the team's rooms: %w", err)
	}
	return rec.ids, nil
}

// join gives every account a token and joins it to the three rooms.
func join(ctx context.Context, adm *matrix.Admin, team *Team, c Clock) error {
	accounts := append([]*Account{&team.Human}, pointers(team.Agents)...)
	for _, a := range accounts {
		token, err := adm.MintToken(ctx, a.UserID)
		if err != nil {
			return fmt.Errorf("a token for %s: %w", a.UserID, err)
		}
		a.Token = token.Reveal()
		cl, err := matrix.NewMember(matrix.Config{BaseURL: team.Homeserver, Token: a.Token, Clock: c})
		if err != nil {
			return err
		}
		for _, room := range []string{team.Rooms.Main, team.Rooms.Reports, team.Rooms.Questions} {
			if _, err := cl.Join(ctx, room); err != nil {
				cl.Close()
				return fmt.Errorf("%s joins %s: %w", a.UserID, room, err)
			}
		}
		cl.Close()
	}
	return nil
}

func pointers(list []Account) []*Account {
	out := make([]*Account, len(list))
	for i := range list {
		out[i] = &list[i]
	}
	return out
}

// WriteTeam writes the team file into dir, a new directory made 0700, as 0600.
func WriteTeam(dir string, t Team) error {
	data, err := json.MarshalIndent(t, "", "  ")
	if err != nil {
		return err
	}
	if err := os.Mkdir(dir, 0o700); err != nil {
		return fmt.Errorf("the team's directory must be a new directory: %w", err)
	}
	return os.WriteFile(filepath.Join(dir, TeamFile), append(data, '\n'), 0o600)
}

// ReadTeam reads the team file in dir and checks it (LG3).
func ReadTeam(dir string) (Team, error) {
	data, err := os.ReadFile(filepath.Join(filepath.Clean(dir), TeamFile))
	if err != nil {
		return Team{}, err
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	var t Team
	if err := dec.Decode(&t); err != nil {
		return Team{}, fmt.Errorf("the team file: %w", err)
	}
	return t, t.check()
}

var _ Clock = clock.System()
