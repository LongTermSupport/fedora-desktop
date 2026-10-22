package proto

// Anchors are what the bundle pins (A 6.2): nothing in a room changes them.
type Anchors struct {
	Server, Team, MainRoom, Self string
}

// TrustError is a room that fails verification (A 6.4). Check names the check broken, never a
// value from the room.
type TrustError struct {
	Purpose, Check string
}

func (e *TrustError) Error() string { return "teamchat " + e.Purpose + " room unverified: " + e.Check }

// Matrix's defaults for absent power-level keys (client-server spec, m.room.power_levels).
const (
	defaultStateLevel = 50
	defaultOpLevel    = 50
	defaultInvite     = 0
	powerLevelsCheck  = "power levels"
)

// VerifyMain verifies the main room from its full state (A 6.4) and returns its roster: the
// create event, the join rule `invite`, the power-level invariants, and a valid roster the
// admin wrote that lists this member, not parked.
func VerifyMain(state []StateEvent, a Anchors) (Roster, error) {
	fail := func(check string) (Roster, error) { return Roster{}, &TrustError{Purpose: PurposeMain, Check: check} }
	if check := verifyCommon(a.MainRoom, state, a, Obj(M("join_rule", Str("invite")))); check != "" {
		return fail(check)
	}
	ev, ok := adminState(state, RosterType, a.Server)
	if !ok {
		return fail("roster")
	}
	r, err := DecodeRoster(ev.Content, a.Server, a.Team, a.MainRoom)
	if err != nil {
		return fail("roster")
	}
	if m, listed := r.Members[a.Self]; !listed || m.Parked {
		return fail("member")
	}
	return r, nil
}

// VerifySide verifies a side room against the main room's verified roster (A 6.4, 5.3): the
// same create and power-level checks, its join rule, the roster naming it under its purpose,
// and a link from the admin naming this purpose, this team and this member's main room.
func VerifySide(purpose, roomID string, state []StateEvent, r Roster, a Anchors) error {
	fail := func(check string) error { return &TrustError{Purpose: purpose, Check: check} }
	join := Obj(M("join_rule", Str("invite")))
	switch purpose {
	case PurposeReports:
		join = Obj(M("join_rule", Str("restricted")), M("allow", Arr(Obj(
			M("type", Str("m.room_membership")), M("room_id", Str(a.MainRoom)),
		))))
		if r.Rooms.Reports != roomID {
			return fail("roster")
		}
	case PurposeQuestions:
		if r.Rooms.Questions != roomID {
			return fail("roster")
		}
	default:
		return fail("roster")
	}
	if check := verifyCommon(roomID, state, a, join); check != "" {
		return fail(check)
	}
	ev, ok := adminState(state, LinkType, a.Server)
	if !ok || !ev.Content.Equal(link(a.Team, purpose, a.MainRoom).Content) {
		return fail("link")
	}
	return nil
}

// verifyCommon is the create, join-rule and power-level checks every room shares: the broken
// check's name, or "". The join rule must equal join exactly.
func verifyCommon(roomID string, state []StateEvent, a Anchors, join Value) string {
	if !ValidRoomID(roomID) {
		return "create"
	}
	create, ok := adminState(state, CreateType, a.Server)
	cf := readFields(create.Content, "create")
	if !ok || cf.str("room_version", true, "create") != RoomVersion || cf.has("additional_creators") || cf.rule != "" ||
		create.EventID != "$"+roomID[1:] {
		return "create"
	}
	if ev, found := adminState(state, JoinRulesType, a.Server); !found || !ev.Content.Equal(join) {
		return "join rule"
	}
	power, found := adminState(state, PowerLevelsType, a.Server)
	if !found {
		return powerLevelsCheck
	}
	return powerLevelsBroken(power.Content, a.Server)
}

// adminState is the one state event of a type under key "" sent by the admin; false when it is
// missing, repeated or from anyone else.
func adminState(state []StateEvent, typ, server string) (StateEvent, bool) {
	var found StateEvent
	n := 0
	for _, e := range state {
		if e.Type == typ {
			found = e
			n++
		}
	}
	return found, n == 1 && found.StateKey == "" && found.Sender == AdminID(server)
}

// CheckPowerLevels holds a power-levels content to A 6.4's invariants I1-I4 over top, the
// highest level of any user but the creator. Absent keys take Matrix's defaults; a level that
// is not an integer fails.
func CheckPowerLevels(content Value, server string) error {
	if check := powerLevelsBroken(content, server); check != "" {
		return &TrustError{Purpose: "", Check: check}
	}
	return nil
}

// powerLevelsBroken is the name of the invariant broken, or "".
func powerLevelsBroken(content Value, server string) string {
	f := readFields(content, powerLevelsCheck)
	top := f.integer("users_default", false, 0, powerLevelsCheck)
	for _, m := range f.optObject("users", powerLevelsCheck).members {
		if m.Val.kind != KindInt {
			f.fail(powerLevelsCheck)
		}
		if m.Key != AdminID(server) {
			top = max(top, m.Val.i)
		}
	}
	state := f.integer("state_default", false, defaultStateLevel, powerLevelsCheck)
	ops := []int64{
		f.integer("invite", false, defaultInvite, powerLevelsCheck),
		f.integer("kick", false, defaultOpLevel, powerLevelsCheck),
		f.integer("ban", false, defaultOpLevel, powerLevelsCheck),
		f.integer("redact", false, defaultOpLevel, powerLevelsCheck),
	}
	eventsDefault := f.integer("events_default", false, 0, powerLevelsCheck)
	events := f.optObject("events", powerLevelsCheck)
	switch {
	case f.rule != "":
		return f.rule
	case state <= top:
		return "power levels I1"
	case min(ops[0], ops[1], ops[2], ops[3]) <= top:
		return "power levels I2"
	}
	for _, m := range events.members {
		if m.Key != MessageType && (m.Val.kind != KindInt || m.Val.i <= top) {
			return "power levels I3"
		}
	}
	if eventsDefault <= top {
		return "power levels I4"
	}
	return ""
}

// InviteAction is what a member does with an invite (A 6.3).
type InviteAction int

// The three answers to an invite.
const (
	InviteReject InviteAction = iota
	InviteAccept
	InvitePending
)

// Invite applies A 6.3's invite rule. roster is main's verified roster, nil while main is not
// verified. An admin invite to a room the verified roster does not name yet is left pending,
// so a roster write racing it costs no re-invite; reports takes no invite (members join it by
// the restricted rule).
func Invite(roomID, inviter, createSender string, a Anchors, roster *Roster) InviteAction {
	admin := AdminID(a.Server)
	fromAdmin := inviter == admin && createSender == admin
	switch {
	case roomID == a.MainRoom:
		if fromAdmin {
			return InviteAccept
		}
		return InviteReject
	case !fromAdmin:
		return InviteReject
	case roster == nil:
		return InvitePending
	case roomID == roster.Rooms.Questions:
		if coordinator, ok := roster.Coordinator(); ok && coordinator == a.Self {
			return InviteAccept
		}
		return InviteReject
	case roomID == roster.Rooms.Reports:
		return InviteReject
	}
	return InvitePending
}
