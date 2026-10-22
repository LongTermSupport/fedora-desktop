package admin

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/apply"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/matrix"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/matrix/fakehs"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/proto"
)

const stagedPort = 8449

func strList(items ...string) proto.Value {
	out := make([]proto.Value, 0, len(items))
	for _, s := range items {
		out = append(out, proto.Str(s))
	}
	return proto.Arr(out...)
}

func (w *world) roomsArg(t *testing.T, ids proto.RoomIDs) proto.Member {
	t.Helper()
	return proto.M(keyRooms, parsed(t, apply.EncodeRooms(alpha(t), ids)))
}

func rosterArg(t *testing.T, f apply.RosterFile) proto.Member {
	t.Helper()
	return proto.M(keyRoster, parsed(t, f.Encode()))
}

func (w *world) removeRooms(t *testing.T) {
	t.Helper()
	if err := os.Remove(w.root + paths.RoomsFile(alpha(t))); err != nil {
		t.Fatal(err)
	}
}

// liveRoster is the roster event main serves now.
func (w *world) liveRoster(t *testing.T) proto.Roster {
	t.Helper()
	main := w.rooms(t).Main
	raw, err := w.admin.GetState(t.Context(), main, proto.RosterType, "")
	if err != nil {
		t.Fatal(err)
	}
	r, err := proto.DecodeRoster(parsed(t, raw), server, "alpha", main)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

// merged is the roster a restore secures: one rev above the live event, epoch 7, the third seat
// retired.
func (w *world) merged(t *testing.T) apply.RosterFile {
	t.Helper()
	f := w.roster(t)
	f.Rev = w.liveRoster(t).Rev + 1
	f.RestoreEpoch = 7
	m := f.Members[sampleHandle]
	m.State = apply.MemberRetired
	f.Members[sampleHandle] = m
	return f
}

func (w *world) secureArgs(t *testing.T, f apply.RosterFile) []proto.Member {
	t.Helper()
	return []proto.Member{
		w.teamFileArg(t), proto.M(keyPort, proto.Int(stagedPort)), proto.M(keyToken, proto.Str(w.adminToken(t))),
		w.roomsArg(t, w.rooms(t)), rosterArg(t, f),
		proto.M(keyRetire, strList(sampleHandle)), proto.M(keyRevoke, strList(coord)),
		proto.M(keyRevokeHumans, strList("alice")), proto.M(keyLock, strList("bob")),
	}
}

// adminToken is a fresh token of the team's admin, the tar's as the staged copy knows it.
func (w *world) adminToken(t *testing.T) string {
	t.Helper()
	token, err := w.srv.MintToken(proto.AdminID(server))
	if err != nil {
		t.Fatal(err)
	}
	return token
}

func (w *world) account(t *testing.T, localpart string) matrix.Account {
	t.Helper()
	a, found, err := w.admin.Account(t.Context(), proto.UserID(localpart, server))
	if err != nil || !found {
		t.Fatalf("account %s: %v %v", localpart, found, err)
	}
	return a
}

func (w *world) joinedOrInvited(t *testing.T, localpart string) []string {
	t.Helper()
	var out []string
	ids := w.rooms(t)
	for _, room := range []string{ids.Main, ids.Reports, ids.Questions} {
		m, err := w.admin.Membership(t.Context(), room, proto.UserID(localpart, server))
		if err != nil {
			t.Fatal(err)
		}
		if m == "join" || m == "invite" {
			out = append(out, room)
		}
	}
	return out
}

// TestServeRestoreSecureRetiresRevokesLocksThenWritesTheRoster is RC1 on the staged server.
func TestServeRestoreSecureRetiresRevokesLocksThenWritesTheRoster(t *testing.T) {
	w := withMember(t)
	if len(w.joinedOrInvited(t, sampleHandle)) == 0 {
		t.Fatal("fixture: the third seat is in no room")
	}
	f := w.merged(t)
	a := w.serve(t, OpRestoreSecure, w.secureArgs(t, f)...)
	if !a.OK || answerFits(OpRestoreSecure, a) != nil {
		t.Fatalf("answer %+v", a)
	}
	if got := w.joinedOrInvited(t, sampleHandle); len(got) != 0 {
		t.Errorf("the retired member is still in %v", got)
	}
	if !w.account(t, sampleHandle).Deactivated {
		t.Error("the retired member is not deactivated")
	}
	if w.signsIn(t, coord, "pw") {
		t.Error("the revoked member's password still works")
	}
	if !w.account(t, "bob").Locked || w.account(t, "alice").Locked {
		t.Error("bob is not locked, or alice is")
	}
	w.wantSecuredRoster(t, f)
	want := []string{"member " + sampleHandle + " deactivated", "member " + coord + " signed out", "human alice signed out", "human bob locked", "state " + proto.RosterType}
	for _, line := range want {
		if !slices.Contains(a.Changed, line) {
			t.Errorf("changed %q lacks %q", a.Changed, line)
		}
	}
	if a.Changed[len(a.Changed)-1] != "state "+proto.RosterType {
		t.Errorf("the roster event is not written last: %q", a.Changed)
	}
	if !slices.Equal(w.stagedPorts, []int{stagedPort}) {
		t.Errorf("staged ports %v", w.stagedPorts)
	}
}

// wantSecuredRoster checks main serves f's roster: its rev, epoch 7, the retired seat out, and
// the side rooms.
func (w *world) wantSecuredRoster(t *testing.T, f apply.RosterFile) {
	t.Helper()
	got := w.liveRoster(t)
	if got.Rev != f.Rev || got.RestoreEpoch != 7 {
		t.Errorf("roster event rev %d epoch %d, want %d 7", got.Rev, got.RestoreEpoch, f.Rev)
	}
	if _, in := got.Members[proto.UserID(sampleHandle, server)]; in {
		t.Error("the retired member is in the roster event")
	}
	if got.Rooms.Reports != w.rooms(t).Reports {
		t.Errorf("roster rooms %+v", got.Rooms)
	}
}

// TestServeRestoreSecureAgainChangesOnlyWhatIsLeft is RC1 repeated: a retired member already
// out and deactivated is not touched again.
func TestServeRestoreSecureAgainChangesOnlyWhatIsLeft(t *testing.T) {
	w := withMember(t)
	f := w.merged(t)
	if a := w.serve(t, OpRestoreSecure, w.secureArgs(t, f)...); !a.OK {
		t.Fatalf("first %+v", a)
	}
	f.Rev++
	a := w.serve(t, OpRestoreSecure, w.secureArgs(t, f)...)
	if !a.OK || slices.ContainsFunc(a.Changed, func(l string) bool { return strings.Contains(l, sampleHandle) }) {
		t.Fatalf("again %+v", a)
	}
}

// TestServeRestoreSecureRefusesARosterNotAboveTheStagedRev is RC1's rev rule, checked before
// anything is changed.
func TestServeRestoreSecureRefusesARosterNotAboveTheStagedRev(t *testing.T) {
	w := withMember(t)
	f := w.merged(t)
	f.Rev--
	a := w.serve(t, OpRestoreSecure, w.secureArgs(t, f)...)
	if a.OK || a.Error.Class != ClassRefused || !strings.Contains(a.Error.Message, "rev") {
		t.Fatalf("answer %+v", a)
	}
	if w.account(t, sampleHandle).Deactivated || len(w.joinedOrInvited(t, sampleHandle)) == 0 {
		t.Error("the member was retired before the refusal")
	}
}

// TestServeRestoreSecureFailures is RC1's failures: the staged server unreachable or refusing,
// a roster A's rules refuse, and each call the homeserver forbids.
func TestServeRestoreSecureFailures(t *testing.T) {
	t.Run("unreachable", func(t *testing.T) {
		w := withMember(t)
		w.stagedErr = matrix.ErrUnreachable
		if a := w.serve(t, OpRestoreSecure, w.secureArgs(t, w.merged(t))...); a.OK || a.Error.Class != ClassUnreachable {
			t.Fatalf("answer %+v", a)
		}
	})
	t.Run("staged refuses", func(t *testing.T) {
		w := withMember(t)
		w.stagedErr = errors.New("a bad token")
		if a := w.serve(t, OpRestoreSecure, w.secureArgs(t, w.merged(t))...); a.OK || a.Error.Class != ClassFailed {
			t.Fatalf("answer %+v", a)
		}
	})
	t.Run("roster", func(t *testing.T) {
		w := withMember(t)
		f := w.merged(t)
		f.Members[sampleHandle] = apply.Member{State: apply.MemberActive, Kind: "podman"}
		f.Coordinator = sampleHandle
		if a := w.serve(t, OpRestoreSecure, w.secureArgs(t, f)...); a.OK || a.Error.Class != ClassFailed {
			t.Fatalf("answer %+v", a)
		}
	})
	for _, op := range []struct{ method, path string }{
		{"POST", "/kick"},
		{"PUT", "/_synapse"},
		{"GET", "/state/m.room.member"},
		{"GET", "/state/teamchat.roster"},
		{"PUT", "/state/teamchat.roster"},
	} {
		t.Run(op.method+" "+op.path, func(t *testing.T) {
			w := withMember(t)
			args := w.secureArgs(t, w.merged(t))
			w.srv.Inject(fakehs.Fault{Method: op.method, Path: op.path, Status: 403, Body: []byte(`{"errcode":"M_FORBIDDEN"}`), Times: 1000})
			if a := w.serve(t, OpRestoreSecure, args...); a.OK || a.Error.Class != ClassFailed {
				t.Fatalf("answer %+v", a)
			}
		})
	}
}

func (w *world) checkArgs(t *testing.T, port, token proto.Value) []proto.Member {
	t.Helper()
	return []proto.Member{w.teamFileArg(t), proto.M(keyPort, port), proto.M(keyToken, token)}
}

// TestServeRestoreCheckReadsTheServedRosterEvent is RC8: the live server with null port and
// token, the staged one through Staged.
func TestServeRestoreCheckReadsTheServedRosterEvent(t *testing.T) {
	w := withMember(t)
	live := w.liveRoster(t)
	a := w.serve(t, OpRestoreCheck, w.checkArgs(t, proto.Null(), proto.Null())...)
	if !a.OK || a.Epoch == nil || a.Epoch.Rev != live.Rev || a.Epoch.RestoreEpoch != live.RestoreEpoch || len(w.stagedPorts) != 0 {
		t.Fatalf("live answer %+v %v", a, w.stagedPorts)
	}
	if secured := w.serve(t, OpRestoreSecure, w.secureArgs(t, w.merged(t))...); !secured.OK {
		t.Fatalf("secure %+v", secured)
	}
	a = w.serve(t, OpRestoreCheck, w.checkArgs(t, proto.Int(stagedPort), proto.Str(w.adminToken(t)))...)
	if !a.OK || a.Epoch.RestoreEpoch != 7 || a.Epoch.Rev != live.Rev+1 || answerFits(OpRestoreCheck, a) != nil {
		t.Fatalf("staged answer %+v", a)
	}
	if _, err := EncodeAnswer(a); err != nil {
		t.Fatal(err)
	}
}

// TestServeRestoreCheckRefusesWhatIsNotTheAdminsRoster is RC8 decoding by A's rules: an event
// another account set, a roster A refuses, an event that is not an object.
func TestServeRestoreCheckRefusesWhatIsNotTheAdminsRoster(t *testing.T) {
	live := func(t *testing.T, w *world) []proto.Member {
		t.Helper()
		return w.checkArgs(t, proto.Null(), proto.Null())
	}
	t.Run("roster", func(t *testing.T) {
		w := withMember(t)
		if _, err := w.admin.PutState(t.Context(), w.rooms(t).Main, proto.RosterType, "", json.RawMessage(`{"v":1}`)); err != nil {
			t.Fatal(err)
		}
		if a := w.serve(t, OpRestoreCheck, live(t, w)...); a.OK || a.Error.Class != ClassFailed {
			t.Fatalf("answer %+v", a)
		}
	})
	t.Run("sender", func(t *testing.T) {
		w := withMember(t)
		w.srv.Inject(fakehs.Fault{Method: "GET", Path: "/state/teamchat.roster", Status: 200, Body: []byte(`{"sender":"@bob:` + server + `","content":{}}`), Times: 1})
		if a := w.serve(t, OpRestoreCheck, live(t, w)...); a.OK || !strings.Contains(a.Error.Message, "sender") {
			t.Fatalf("answer %+v", a)
		}
	})
	t.Run("shape", func(t *testing.T) {
		w := withMember(t)
		w.srv.Inject(fakehs.Fault{Method: "GET", Path: "/state/teamchat.roster", Status: 200, Body: []byte(`{"sender":1}`), Times: 1})
		if a := w.serve(t, OpRestoreCheck, live(t, w)...); a.OK {
			t.Fatalf("answer %+v", a)
		}
	})
	t.Run("not found", func(t *testing.T) {
		w := withMember(t)
		w.srv.Inject(fakehs.Fault{Method: "GET", Path: "/state/teamchat.roster", Status: 404, Body: []byte(`{"errcode":"M_NOT_FOUND"}`), Times: 1})
		if a := w.serve(t, OpRestoreCheck, live(t, w)...); a.OK || a.Error.Class != ClassFailed {
			t.Fatalf("answer %+v", a)
		}
	})
	t.Run("no rooms record", func(t *testing.T) {
		w := withMember(t)
		w.removeRooms(t)
		if a := w.serve(t, OpRestoreCheck, live(t, w)...); a.OK || a.Error.Class != ClassConfig {
			t.Fatalf("answer %+v", a)
		}
	})
	t.Run("admin client", func(t *testing.T) {
		w := withMember(t)
		w.crash = &crashing{method: "none"}
		if a := w.serve(t, OpRestoreCheck, live(t, w)...); a.OK || a.Error.Class != ClassConfig {
			t.Fatalf("answer %+v", a)
		}
	})
}

// TestServeRestoreSwapHandsTheFilesTheirNewContent is RC2: the token as given, the rooms record
// and roster file in their own encodings.
func TestServeRestoreSwapHandsTheFilesTheirNewContent(t *testing.T) {
	w := withMember(t)
	f := w.merged(t)
	ids := w.rooms(t)
	args := []proto.Member{w.teamFileArg(t), proto.M(keyToken, proto.Str("tar-token")), w.roomsArg(t, ids), rosterArg(t, f)}
	a := w.serve(t, OpRestoreSwap, args...)
	if !a.OK || len(a.Changed) == 0 {
		t.Fatalf("answer %+v", a)
	}
	if string(w.files.token) != "tar-token" || string(w.files.rooms) != string(apply.EncodeRooms(alpha(t), ids)) || string(w.files.roster) != string(f.Encode()) {
		t.Fatalf("files %q %q %q", w.files.token, w.files.rooms, w.files.roster)
	}
	w.files.err = errors.New("disk full")
	if a := w.serve(t, OpRestoreSwap, args...); a.OK || a.Error.Class != ClassFailed {
		t.Fatalf("a failed swap %+v", a)
	}
}

// TestServeRestoreFinishAndRevertAnswerTheirNames is RC4 and RC5.
func TestServeRestoreFinishAndRevertAnswerTheirNames(t *testing.T) {
	w := withMember(t)
	w.files.previous = []string{"admin.token", "rooms.json"}
	if a := w.serve(t, OpRestoreFinish, w.teamFileArg(t)); !a.OK || !slices.Equal(a.Changed, []string{"admin.token.previous removed", "rooms.json.previous removed"}) {
		t.Fatalf("finish %+v", a)
	}
	if a := w.serve(t, OpRestoreRevert, w.teamFileArg(t)); !a.OK || !slices.Equal(a.Changed, []string{"admin.token put back", "rooms.json put back"}) {
		t.Fatalf("revert %+v", a)
	}
	w.files.previous = nil
	if a := w.serve(t, OpRestoreRevert, w.teamFileArg(t)); !a.OK || len(a.Changed) != 0 {
		t.Fatalf("revert of nothing %+v", a)
	}
	w.files.err = errors.New("eio")
	for _, op := range []string{OpRestoreFinish, OpRestoreRevert} {
		if a := w.serve(t, op, w.teamFileArg(t)); a.OK || a.Error.Class != ClassFailed {
			t.Fatalf("%s failing %+v", op, a)
		}
	}
}

// send posts one text message to room as the admin and answers its server time.
func (w *world) send(t *testing.T, room, body string) int64 {
	t.Helper()
	id, err := w.admin.Send(t.Context(), room, "m.room.message", "txn-"+body, json.RawMessage(`{"msgtype":"m.text","body":"`+body+`"}`))
	if err != nil {
		t.Fatal(err)
	}
	raw, err := w.admin.GetEvent(t.Context(), room, id)
	if err != nil {
		t.Fatal(err)
	}
	ts, ok := parsed(t, raw).Get("origin_server_ts")
	if !ok {
		t.Fatal("no origin_server_ts")
	}
	return ts.Int()
}

// bodiesOf are the message bodies of an export, in its order.
func bodiesOf(t *testing.T, data []byte) []string {
	t.Helper()
	var out []string
	for line := range strings.Lines(string(data)) {
		content, ok := parsed(t, []byte(line)).Get("content")
		if !ok {
			continue
		}
		if body, ok := content.Get("body"); ok {
			out = append(out, body.Str())
		}
	}
	return out
}

// TestServeRestoreExportWritesWhatTheRestoreLoses is RC3: every event after since_ms in the
// team's rooms, in time order, with its count, first and end.
func TestServeRestoreExportWritesWhatTheRestoreLoses(t *testing.T) {
	w := withMember(t)
	ids := w.rooms(t)
	w.send(t, ids.Main, "before")
	since := w.send(t, ids.Reports, "at-since")
	first := w.send(t, ids.Main, "one")
	w.send(t, ids.Reports, "two")
	w.send(t, ids.Main, "three")
	a := w.serve(t, OpRestoreExport, w.teamFileArg(t), proto.M(keyBackup, proto.Int(4)), proto.M(keySinceMS, proto.Int(since)))
	if !a.OK || a.Export == nil || answerFits(OpRestoreExport, a) != nil {
		t.Fatalf("answer %+v", a)
	}
	data := w.files.lost[4]
	if got := bodiesOf(t, data); !slices.Equal(got, []string{"one", "two", "three"}) {
		t.Errorf("bodies %v", got)
	}
	lines := strings.Count(string(data), "\n")
	if a.Export.Count != int64(lines) || a.Export.FirstMS != first || a.Export.EndMS < first || a.Export.Truncated {
		t.Errorf("export %+v, %d lines, first %d", a.Export, lines, first)
	}
	if _, err := EncodeAnswer(a); err != nil {
		t.Fatal(err)
	}
}

// TestServeRestoreExportOfNothingIsEmpty is RC3 when nothing came after the backup.
func TestServeRestoreExportOfNothingIsEmpty(t *testing.T) {
	w := withMember(t)
	since := w.send(t, w.rooms(t).Main, "last")
	a := w.serve(t, OpRestoreExport, w.teamFileArg(t), proto.M(keyBackup, proto.Int(1)), proto.M(keySinceMS, proto.Int(since)))
	if !a.OK || a.Export.Count != 0 || a.Export.FirstMS != 0 || len(w.files.lost[1]) != 0 {
		t.Fatalf("answer %+v, %q", a, w.files.lost[1])
	}
	if _, err := EncodeAnswer(a); err != nil {
		t.Fatal(err)
	}
}

// TestExportKeepsTheNewestUnderTheCap is RC3's cap (n2g-D15): reading backwards, the newest
// events are kept and the answer says it was cut.
func TestExportKeepsTheNewestUnderTheCap(t *testing.T) {
	w := withMember(t)
	ids := w.rooms(t)
	var since int64
	for i := range 6 {
		ts := w.send(t, []string{ids.Main, ids.Reports}[i%2], "m"+strconv.Itoa(i))
		if i == 0 {
			since = ts - 1
		}
	}
	one := len(w.messageLine(t, ids.Main)) + 1
	ex, data, err := exportEvents(t.Context(), w.admin, ids, since, 3*one+one/2, 2)
	if err != nil {
		t.Fatal(err)
	}
	if got := bodiesOf(t, data); !slices.Equal(got, []string{"m3", "m4", "m5"}) {
		t.Errorf("kept %v", got)
	}
	if !ex.Truncated || ex.Count != 3 {
		t.Errorf("export %+v", ex)
	}
	ex, data, err = exportEvents(t.Context(), w.admin, ids, since, 1<<20, 2)
	if err != nil || ex.Truncated || ex.Count != 6 || len(bodiesOf(t, data)) != 6 {
		t.Fatalf("uncut export %+v %v", ex, err)
	}
}

// messageLine is the newest event of room as the export writes it.
func (w *world) messageLine(t *testing.T, room string) []byte {
	t.Helper()
	page, err := w.admin.Messages(t.Context(), room, matrix.MessagesRequest{Dir: matrix.Backward, Limit: 1})
	if err != nil || len(page.Chunk) != 1 {
		t.Fatalf("messages %v %v", page, err)
	}
	return parsed(t, page.Chunk[0]).Compact()
}

// TestServeRestoreExportFailures is RC3's failures: the homeserver, an event with no time, the
// write, and an admin client that cannot read.
func TestServeRestoreExportFailures(t *testing.T) {
	args := func(t *testing.T, w *world) []proto.Member {
		t.Helper()
		return []proto.Member{w.teamFileArg(t), proto.M(keyBackup, proto.Int(1)), proto.M(keySinceMS, proto.Int(0))}
	}
	t.Run("unreachable", func(t *testing.T) {
		w := withMember(t)
		w.srv.Inject(fakehs.Fault{Method: "GET", Path: "/messages", Drop: true, Times: 1000})
		if a := w.serve(t, OpRestoreExport, args(t, w)...); a.OK || a.Error.Class != ClassUnreachable {
			t.Fatalf("answer %+v", a)
		}
	})
	t.Run("no time", func(t *testing.T) {
		w := withMember(t)
		w.srv.Inject(fakehs.Fault{Method: "GET", Path: "/messages", Status: 200, Body: []byte(`{"chunk":[{"type":"m.room.message"}],"start":"1","end":"0"}`), Times: 1})
		if a := w.serve(t, OpRestoreExport, args(t, w)...); a.OK || !strings.Contains(a.Error.Message, "origin_server_ts") {
			t.Fatalf("answer %+v", a)
		}
	})
	t.Run("write", func(t *testing.T) {
		w := withMember(t)
		w.files.err = errors.New("disk full")
		if a := w.serve(t, OpRestoreExport, args(t, w)...); a.OK || a.Error.Class != ClassFailed {
			t.Fatalf("answer %+v", a)
		}
	})
	t.Run("no rooms record", func(t *testing.T) {
		w := withMember(t)
		w.removeRooms(t)
		if a := w.serve(t, OpRestoreExport, args(t, w)...); a.OK || a.Error.Class != ClassConfig {
			t.Fatalf("answer %+v", a)
		}
	})
	t.Run("admin client", func(t *testing.T) {
		w := withMember(t)
		w.crash = &crashing{method: "none"}
		if a := w.serve(t, OpRestoreExport, args(t, w)...); a.OK || a.Error.Class != ClassConfig {
			t.Fatalf("answer %+v", a)
		}
	})
}

// TestServeRestoreArgsAreStrict is CV1 for the restore ops: exact keys, and each value of its
// kind and grammar.
func TestServeRestoreArgsAreStrict(t *testing.T) {
	w := withMember(t)
	f := w.merged(t)
	base := w.secureArgs(t, f)
	with := func(key string, v proto.Value) []proto.Member {
		out := slices.Clone(base)
		for i := range out {
			if out[i].Key == key {
				out[i].Val = v
			}
		}
		return out
	}
	for name, args := range map[string][]proto.Member{
		"missing lock":     base[:len(base)-1],
		"extra key":        append(slices.Clone(base), proto.M("x", proto.Int(1))),
		"port zero":        with(keyPort, proto.Int(0)),
		"port high":        with(keyPort, proto.Int(65536)),
		"port string":      with(keyPort, proto.Str("8449")),
		"token number":     with(keyToken, proto.Int(1)),
		"token space":      with(keyToken, proto.Str("a b")),
		"rooms":            with(keyRooms, proto.Obj()),
		"roster":           with(keyRoster, proto.Obj()),
		"retire string":    with(keyRetire, proto.Str(sampleHandle)),
		"retire bad":       with(keyRetire, strList("Not A Handle")),
		"retire number":    with(keyRetire, proto.Arr(proto.Int(1))),
		"revoke humans":    with(keyRevokeHumans, strList(sampleHandle)),
		"lock bad":         with(keyLock, strList("")),
		"team file":        with(keyTeamFile, proto.Obj()),
		"rooms of another": with(keyRooms, parsed(t, []byte(`{"v":1,"team":"beta"}`))),
	} {
		if a := w.serve(t, OpRestoreSecure, args...); a.OK || a.Error.Class != ClassFailed {
			t.Errorf("%s: %+v", name, a)
		}
	}
	tf := w.teamFileArg(t)
	for name, c := range map[string]struct {
		op   string
		args []proto.Member
	}{
		"check port":      {OpRestoreCheck, w.checkArgs(t, proto.Int(70000), proto.Str("t"))},
		"check half null": {OpRestoreCheck, w.checkArgs(t, proto.Null(), proto.Str("t"))},
		"check token":     {OpRestoreCheck, w.checkArgs(t, proto.Int(stagedPort), proto.Null())},
		"export backup":   {OpRestoreExport, []proto.Member{tf, proto.M(keyBackup, proto.Int(0)), proto.M(keySinceMS, proto.Int(0))}},
		"export since":    {OpRestoreExport, []proto.Member{tf, proto.M(keyBackup, proto.Int(1)), proto.M(keySinceMS, proto.Int(-1))}},
		"export string":   {OpRestoreExport, []proto.Member{tf, proto.M(keyBackup, proto.Str("1")), proto.M(keySinceMS, proto.Int(0))}},
		"swap token":      {OpRestoreSwap, []proto.Member{tf, proto.M(keyToken, proto.Str("")), w.roomsArg(t, w.rooms(t)), rosterArg(t, f)}},
		"swap rooms":      {OpRestoreSwap, []proto.Member{tf, proto.M(keyToken, proto.Str("t")), proto.M(keyRooms, proto.Int(1)), rosterArg(t, f)}},
		"swap roster":     {OpRestoreSwap, []proto.Member{tf, proto.M(keyToken, proto.Str("t")), w.roomsArg(t, w.rooms(t)), proto.M(keyRoster, proto.Arr())}},
		"finish extra":    {OpRestoreFinish, []proto.Member{tf, proto.M(keyToken, proto.Str("t"))}},
	} {
		if a := w.serve(t, c.op, c.args...); a.OK || a.Error.Class != ClassFailed {
			t.Errorf("%s: %+v", name, a)
		}
	}
	if w.files.token != nil {
		t.Error("a bad swap reached the files")
	}
}

// openThen is the world's Opener with change applied to what it opens.
func (w *world) openThen(change func(Opened) (Opened, error)) Opener {
	return func(ctx context.Context, team paths.TeamID, port int, regKey string) (Opened, error) {
		o, err := w.open(ctx, team, port, regKey)
		if err != nil {
			return Opened{}, err
		}
		return change(o)
	}
}

// TestServeRestoreNeedsItsSeams is the Opener's failure and an Opened without restore files or
// a staged client: the child's own setup (78).
func TestServeRestoreNeedsItsSeams(t *testing.T) {
	w := withMember(t)
	tf := w.teamFileArg(t)
	f := w.merged(t)
	bare := w.openThen(func(o Opened) (Opened, error) { return Opened{Deps: o.Deps}, nil })
	failing := w.openThen(func(Opened) (Opened, error) { return Opened{}, errors.New("no directory") })
	for name, c := range map[string]struct {
		op   string
		args []proto.Member
	}{
		"secure": {OpRestoreSecure, w.secureArgs(t, f)},
		"check":  {OpRestoreCheck, w.checkArgs(t, proto.Int(stagedPort), proto.Str("t"))},
		"swap":   {OpRestoreSwap, []proto.Member{tf, proto.M(keyToken, proto.Str("t")), w.roomsArg(t, w.rooms(t)), rosterArg(t, f)}},
		"export": {OpRestoreExport, []proto.Member{tf, proto.M(keyBackup, proto.Int(1)), proto.M(keySinceMS, proto.Int(0))}},
		"finish": {OpRestoreFinish, []proto.Member{tf}},
		"revert": {OpRestoreRevert, []proto.Member{tf}},
	} {
		for how, open := range map[string]Opener{"bare": bare, "failing": failing} {
			a := Serve(t.Context(), open, Request{Op: c.op, Team: alpha(t), Args: proto.Obj(c.args...)})
			if a.OK || a.Error.Class != ClassConfig {
				t.Errorf("%s %s: %+v", name, how, a)
			}
		}
	}
}
