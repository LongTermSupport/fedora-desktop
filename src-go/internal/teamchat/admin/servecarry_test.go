package admin

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/apply"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/carry"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/declare"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/matrix/fakehs"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/proto"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/restore"
)

// newest is room's newest event as carry.tail reads it: its id and its server time.
func (w *world) newest(t *testing.T, purpose, room string) carry.TailEntry {
	t.Helper()
	v := parsed(t, w.messageLine(t, room))
	id, iok := v.Get("event_id")
	ts, tok := v.Get("origin_server_ts")
	if !iok || !tok {
		t.Fatal("the newest event has no id or time")
	}
	return carry.TailEntry{Room: purpose, EventID: id.Str(), TsMS: ts.Int()}
}

// wantTail is each room's newest event, in rooms.json's order.
func (w *world) wantTail(t *testing.T) []carry.TailEntry {
	t.Helper()
	ids := w.rooms(t)
	var out []carry.TailEntry
	for _, r := range []struct{ purpose, id string }{{proto.PurposeMain, ids.Main}, {proto.PurposeReports, ids.Reports}, {proto.PurposeQuestions, ids.Questions}} {
		if r.id != "" {
			out = append(out, w.newest(t, r.purpose, r.id))
		}
	}
	return out
}

// port is alpha's port as its applied team.json declares it.
func (w *world) port(t *testing.T) int {
	t.Helper()
	team, err := declare.DecodeTeamFile(w.teamFile(t))
	if err != nil {
		t.Fatal(err)
	}
	return team.Port
}

// roundTrip is the answer as root reads it off the wire.
func roundTrip(t *testing.T, a Answer) Answer {
	t.Helper()
	raw, err := EncodeAnswer(a)
	if err != nil {
		t.Fatal(err)
	}
	got, err := DecodeAnswer(raw)
	if err != nil {
		t.Fatal(err)
	}
	return got
}

// TestServeCarryTailReadsEachRoomsNewestEventAndTheRoster is SC-C1: for each room of
// rooms.json its newest event, and the main room's roster event, on the live server.
func TestServeCarryTailReadsEachRoomsNewestEventAndTheRoster(t *testing.T) {
	w := withMember(t)
	ids := w.rooms(t)
	w.send(t, ids.Main, "one")
	w.send(t, ids.Reports, "two")
	want := w.wantTail(t)
	if len(want) < 2 {
		t.Fatalf("fixture: %d rooms", len(want))
	}
	a := w.serve(t, OpCarryTail, w.teamFileArg(t))
	if !a.OK || answerFits(OpCarryTail, a) != nil {
		t.Fatalf("answer %+v", a)
	}
	live := w.liveRoster(t)
	if !slices.Equal(a.Tail, want) || a.Epoch == nil || *a.Epoch != (restore.Epoch{RestoreEpoch: live.RestoreEpoch, Rev: live.Rev}) {
		t.Fatalf("tail %+v epoch %+v, want %+v and rev %d", a.Tail, a.Epoch, want, live.Rev)
	}
	if got := roundTrip(t, a); !slices.Equal(got.Tail, want) || *got.Epoch != *a.Epoch {
		t.Fatalf("on the wire %+v", got)
	}
	if w.ports[len(w.ports)-1] != w.port(t) {
		t.Errorf("the tail was read on port %d, not the team's", w.ports[len(w.ports)-1])
	}
}

// TestServeCarryTailFailures is SC-C1's failures, each by its class: a server lost mid-read is
// 69 (RS15), the child's files 78, an answer the child cannot read 1.
func TestServeCarryTailFailures(t *testing.T) {
	for name, c := range map[string]struct {
		set  func(t *testing.T, w *world)
		want ErrorClass
	}{
		"unreachable": {func(_ *testing.T, w *world) {
			w.srv.Inject(fakehs.Fault{Method: "GET", Path: "/messages", Drop: true, Times: 1000})
		}, ClassUnreachable},
		"roster unreachable": {func(_ *testing.T, w *world) {
			w.srv.Inject(fakehs.Fault{Method: "GET", Path: "/state/teamchat.roster", Drop: true, Times: 1000})
		}, ClassUnreachable},
		"empty room": {func(_ *testing.T, w *world) {
			w.srv.Inject(fakehs.Fault{Method: "GET", Path: "/messages", Status: 200, Body: []byte(`{"chunk":[],"start":"1"}`), Times: 1})
		}, ClassFailed},
		"no event id": {func(_ *testing.T, w *world) {
			w.srv.Inject(fakehs.Fault{Method: "GET", Path: "/messages", Status: 200, Body: []byte(`{"chunk":[{"type":"m.room.message","origin_server_ts":5}],"start":"1","end":"0"}`), Times: 1})
		}, ClassFailed},
		"no time": {func(_ *testing.T, w *world) {
			w.srv.Inject(fakehs.Fault{Method: "GET", Path: "/messages", Status: 200, Body: []byte(`{"chunk":[{"type":"m.room.message","event_id":"$e"}],"start":"1","end":"0"}`), Times: 1})
		}, ClassFailed},
		"roster sender": {func(_ *testing.T, w *world) {
			w.srv.Inject(fakehs.Fault{Method: "GET", Path: "/state/teamchat.roster", Status: 200, Body: []byte(`{"sender":"@bob:` + server + `","content":{}}`), Times: 1})
		}, ClassFailed},
		"no rooms record": {removeRoomsOf, ClassConfig},
		"admin client":    {func(_ *testing.T, w *world) { w.crash = &crashing{method: "none"} }, ClassConfig},
	} {
		t.Run(name, func(t *testing.T) {
			w := withMember(t)
			c.set(t, w)
			if a := w.serve(t, OpCarryTail, w.teamFileArg(t)); a.OK || a.Error.Class != c.want {
				t.Fatalf("answer %+v", a)
			}
		})
	}
}

// liveTail is the tail the live server holds now.
func liveTail(t *testing.T, w *world) []carry.TailEntry {
	t.Helper()
	return w.wantTail(t)
}

// removeRoomsOf removes w's rooms record, as a table's set.
func removeRoomsOf(t *testing.T, w *world) {
	t.Helper()
	w.removeRooms(t)
}

func (w *world) carryCheckArgs(t *testing.T, port int64, tail []carry.TailEntry) []proto.Member {
	t.Helper()
	return []proto.Member{w.teamFileArg(t), proto.M(keyPort, proto.Int(port)), proto.M(keyTail, carry.EncodeTail(tail))}
}

// TestServeCarryCheckAnswersEachTailEventFoundOrNot is SC-C3: a client on the port given with
// the token in ctl/<team>/; per tail entry found or not, and the roster event's epoch.
func TestServeCarryCheckAnswersEachTailEventFoundOrNot(t *testing.T) {
	w := withMember(t)
	w.send(t, w.rooms(t).Main, "one")
	tail := w.wantTail(t)
	tail[len(tail)-1].EventID = "$gone:" + server
	a := w.serve(t, OpCarryCheck, w.carryCheckArgs(t, stagedPort, tail)...)
	if !a.OK || answerFits(OpCarryCheck, a) != nil {
		t.Fatalf("answer %+v", a)
	}
	want := append(slices.Repeat([]bool{true}, len(tail)-1), false)
	live := w.liveRoster(t)
	if !slices.Equal(a.Found, want) || *a.Epoch != (restore.Epoch{RestoreEpoch: live.RestoreEpoch, Rev: live.Rev}) {
		t.Fatalf("found %v epoch %+v", a.Found, a.Epoch)
	}
	if got := roundTrip(t, a); !slices.Equal(got.Found, want) {
		t.Fatalf("on the wire %+v", got)
	}
	if w.ports[len(w.ports)-1] != stagedPort {
		t.Errorf("the check opened port %d, not the one given", w.ports[len(w.ports)-1])
	}
}

// TestServeCarryCheckFailures: a server lost is 69, a tail naming a room rooms.json has not, or
// a lookup the server refuses otherwise, is 1.
func TestServeCarryCheckFailures(t *testing.T) {
	for name, c := range map[string]struct {
		set  func(t *testing.T, w *world)
		tail func(t *testing.T, w *world) []carry.TailEntry
		want ErrorClass
	}{
		"unreachable": {
			func(_ *testing.T, w *world) {
				w.srv.Inject(fakehs.Fault{Method: "GET", Path: "/event/", Drop: true, Times: 1000})
			},
			liveTail, ClassUnreachable,
		},
		"refused": {
			func(_ *testing.T, w *world) {
				w.srv.Inject(fakehs.Fault{Method: "GET", Path: "/event/", Status: 403, Body: []byte(`{"errcode":"M_FORBIDDEN"}`), Times: 1})
			},
			liveTail, ClassFailed,
		},
		"unknown room": {
			func(*testing.T, *world) {},
			func(*testing.T, *world) []carry.TailEntry {
				return []carry.TailEntry{{Room: "elsewhere", EventID: "$e", TsMS: 1}}
			}, ClassFailed,
		},
		"no rooms record": {
			removeRoomsOf,
			func(*testing.T, *world) []carry.TailEntry {
				return []carry.TailEntry{{Room: proto.PurposeMain, EventID: "$e", TsMS: 1}}
			}, ClassConfig,
		},
	} {
		t.Run(name, func(t *testing.T) {
			w := withMember(t)
			tail := c.tail(t, w)
			c.set(t, w)
			if a := w.serve(t, OpCarryCheck, w.carryCheckArgs(t, stagedPort, tail)...); a.OK || a.Error.Class != c.want {
				t.Fatalf("answer %+v", a)
			}
		})
	}
}

// lostData is the lost export placeArgs names; its bytes follow the request.
const lostData = "{\"a\":1}\n"

// lostEntry is a lost export as carry.place's request names it: its name, size and sha256.
func lostEntry(name, data string) proto.Value {
	sum := sha256.Sum256([]byte(data))
	return proto.Obj(proto.M("name", proto.Str(name)), proto.M("size", proto.Int(int64(len(data)))), proto.M("sha256", proto.Str(hex.EncodeToString(sum[:]))))
}

// placeArgs are carry.place's args: the team's files, one guest token and one lost export.
func (w *world) placeArgs(t *testing.T) []proto.Member {
	t.Helper()
	return []proto.Member{
		w.teamFileArg(t), rosterArg(t, w.roster(t)), w.roomsArg(t, w.rooms(t)), proto.M(keyToken, proto.Str("carried-token")),
		proto.M(keyGuest, proto.Arr(proto.Obj(proto.M("name", proto.Str(sampleHandle)), proto.M("data", proto.Str("guest-token"))))),
		proto.M(keyLost, proto.Arr(lostEntry("restore-3-lost.jsonl", lostData))),
	}
}

// servePlace is carry.place with placeArgs' lost export's bytes as the stream.
func (w *world) servePlace(t *testing.T, args ...proto.Member) Answer {
	t.Helper()
	return w.servePlaceStream(t, lostData, args...)
}

func (w *world) servePlaceStream(t *testing.T, stream string, args ...proto.Member) Answer {
	t.Helper()
	return ServePlace(w.place, Request{Op: OpCarryPlace, Team: alpha(t), Args: proto.Obj(args...)}, strings.NewReader(stream))
}

// TestServeCarryPlaceStreamIsExactlyTheSizes is condition 1 of the ruling (sc-D11): the
// stream after the request is exactly the sum of the lost exports' sizes, each its sha256; a
// short, a long or a wrong stream fails the op.
func TestServeCarryPlaceStreamIsExactlyTheSizes(t *testing.T) {
	for name, stream := range map[string]string{
		"short":           lostData[:len(lostData)-1],
		"empty":           "",
		"long":            lostData + "x",
		"digest mismatch": "{\"a\":2}\n",
	} {
		w := withMember(t)
		if a := w.servePlaceStream(t, stream, w.placeArgs(t)...); a.OK || a.Error.Class != ClassFailed {
			t.Errorf("%s: %+v", name, a)
		}
		if w.placed != nil {
			t.Errorf("%s: placed %q", name, w.placed)
		}
	}
	w := withMember(t)
	args := w.placeArgs(t)
	args[len(args)-1] = proto.M(keyLost, proto.Arr())
	if a := w.servePlaceStream(t, "x", args...); a.OK {
		t.Errorf("a stream with no lost export named was taken: %+v", a)
	}
	if a := w.servePlaceStream(t, "", args...); !a.OK || len(a.Changed) != 4 {
		t.Errorf("no lost export and no stream: %+v", a)
	}
}

// TestServeCarryPlaceWritesEachFile is SC-C2: the files under ctl/<team>/, each in its own
// file's encoding, handed to the placer, and the names written answered with no credential.
func TestServeCarryPlaceWritesEachFile(t *testing.T) {
	w := withMember(t)
	a := w.servePlace(t, w.placeArgs(t)...)
	if !a.OK || answerFits(OpCarryPlace, a) != nil {
		t.Fatalf("answer %+v", a)
	}
	team := alpha(t)
	want := []PlacedFile{
		{Name: "roster.json", Data: w.roster(t).Encode()},
		{Name: "rooms.json", Data: apply.EncodeRooms(team, w.rooms(t))},
		{Name: "admin.token", Data: []byte("carried-token")},
		{Name: "guest/" + sampleHandle + ".token", Data: []byte("guest-token")},
	}
	if !slices.EqualFunc(w.placed, want, func(a, b PlacedFile) bool { return a.Name == b.Name && string(a.Data) == string(b.Data) }) {
		t.Fatalf("placed %q", w.placed)
	}
	if !slices.Equal(w.lostRead, []string{lostData}) {
		t.Fatalf("the lost export read from the stream %q", w.lostRead)
	}
	for _, line := range a.Changed {
		if strings.Contains(line, "token\"") || strings.Contains(line, "carried-token") || strings.Contains(line, "guest-token") {
			t.Errorf("the answer carries a credential: %q", line)
		}
	}
	if !slices.Equal(a.Changed, []string{"roster.json written", "rooms.json written", "admin.token written", "guest/" + sampleHandle + ".token written", "restore-3-lost.jsonl written"}) {
		t.Errorf("changed %q", a.Changed)
	}
}

// TestServeCarryPlaceFailures: the placer's refusal or failure is 1; no placer is the child's
// setup, 78.
func TestServeCarryPlaceFailures(t *testing.T) {
	w := withMember(t)
	w.placeErr = errors.New("roster.json is already there")
	if a := w.servePlace(t, w.placeArgs(t)...); a.OK || a.Error.Class != ClassFailed {
		t.Fatalf("a failing placer %+v", a)
	}
	a := ServePlace(nil, Request{Op: OpCarryPlace, Team: alpha(t), Args: proto.Obj(w.placeArgs(t)...)}, strings.NewReader(lostData))
	if a.OK || a.Error.Class != ClassConfig {
		t.Fatalf("no placer %+v", a)
	}
	if a := w.serve(t, OpCarryPlace, w.placeArgs(t)...); a.OK || a.Error.Class != ClassFailed {
		t.Fatalf("carry.place through the opener %+v", a)
	}
}

// TestServeCarryArgsAreStrict is CV1 for the carry ops: exact keys, each value of its kind and
// grammar, and nothing written on a bad one.
func TestServeCarryArgsAreStrict(t *testing.T) {
	w := withMember(t)
	base := w.placeArgs(t)
	with := func(key string, v proto.Value) []proto.Member {
		out := slices.Clone(base)
		for i := range out {
			if out[i].Key == key {
				out[i].Val = v
			}
		}
		return out
	}
	guest := func(name, data proto.Value) proto.Value {
		return proto.Arr(proto.Obj(proto.M("name", name), proto.M("data", data)))
	}
	lost := func(name, size, sum proto.Value) proto.Value {
		return proto.Arr(proto.Obj(proto.M("name", name), proto.M("size", size), proto.M("sha256", sum)))
	}
	good := lostEntry("restore-3-lost.jsonl", lostData)
	name, size, sum := good.Members()[0].Val, good.Members()[1].Val, good.Members()[2].Val
	for name, args := range map[string][]proto.Member{
		"missing lost":     base[:len(base)-1],
		"extra key":        append(slices.Clone(base), proto.M("x", proto.Int(1))),
		"token":            with(keyToken, proto.Str("a b")),
		"rooms":            with(keyRooms, proto.Obj()),
		"roster":           with(keyRoster, proto.Arr()),
		"guest object":     with(keyGuest, proto.Obj()),
		"guest name":       with(keyGuest, guest(proto.Str("../x"), proto.Str("t"))),
		"guest token":      with(keyGuest, guest(proto.Str(sampleHandle), proto.Str(""))),
		"guest extra":      with(keyGuest, proto.Arr(proto.Obj(proto.M("name", proto.Str(sampleHandle)), proto.M("data", proto.Str("t")), proto.M("x", proto.Int(1))))),
		"guest twice":      with(keyGuest, proto.Arr(guest(proto.Str(sampleHandle), proto.Str("t")).Items()[0], guest(proto.Str(sampleHandle), proto.Str("u")).Items()[0])),
		"lost traversal":   with(keyLost, lost(proto.Str("../restore-3-lost.jsonl"), size, sum)),
		"lost path":        with(keyLost, lost(proto.Str("guest/restore-3-lost.jsonl"), size, sum)),
		"lost other name":  with(keyLost, lost(proto.Str("roster.json"), size, sum)),
		"lost number zero": with(keyLost, lost(proto.Str("restore-0-lost.jsonl"), size, sum)),
		"lost name number": with(keyLost, lost(proto.Int(3), size, sum)),
		"lost size string": with(keyLost, lost(name, proto.Str("8"), sum)),
		"lost size below":  with(keyLost, lost(name, proto.Int(-1), sum)),
		"lost oversize":    with(keyLost, lost(name, proto.Int(carry.LostCap+1), sum)),
		"lost digest":      with(keyLost, lost(name, size, proto.Str(strings.ToUpper(sum.Str())))),
		"lost digest len":  with(keyLost, lost(name, size, proto.Str("abc"))),
		"lost extra":       with(keyLost, proto.Arr(proto.Obj(append(good.Members(), proto.M("data", proto.Str(lostData)))...))),
		"lost data form":   with(keyLost, proto.Arr(proto.Obj(proto.M("n", proto.Int(3)), proto.M("data", proto.Str(lostData))))),
		"lost twice":       with(keyLost, proto.Arr(good, good)),
		"team file":        with(keyTeamFile, proto.Obj()),
	} {
		if a := w.servePlace(t, args...); a.OK || a.Error.Class != ClassFailed {
			t.Errorf("%s: %+v", name, a)
		}
	}
	if w.placed != nil {
		t.Errorf("a bad place reached the placer: %q", w.placed)
	}
	tf := w.teamFileArg(t)
	for name, c := range map[string]struct {
		op   string
		args []proto.Member
	}{
		"tail extra":    {OpCarryTail, []proto.Member{tf, proto.M(keyPort, proto.Int(1))}},
		"check port":    {OpCarryCheck, w.carryCheckArgs(t, 0, w.wantTail(t))},
		"check null":    {OpCarryCheck, []proto.Member{tf, proto.M(keyPort, proto.Null()), proto.M(keyTail, carry.EncodeTail(w.wantTail(t)))}},
		"check no tail": {OpCarryCheck, []proto.Member{tf, proto.M(keyPort, proto.Int(stagedPort)), proto.M(keyTail, proto.Arr())}},
		"check missing": {OpCarryCheck, []proto.Member{tf, proto.M(keyPort, proto.Int(stagedPort))}},
	} {
		if a := w.serve(t, c.op, c.args...); a.OK || a.Error.Class != ClassFailed {
			t.Errorf("%s: %+v", name, a)
		}
	}
}

// TestCarryAnswersFitTheirOps is RC6's rule for the carry ops: a tail answers carry.tail, a
// found list carry.check, an epoch both, each always and only there; carry.place answers lines.
func TestCarryAnswersFitTheirOps(t *testing.T) {
	tail := []carry.TailEntry{{Room: proto.PurposeMain, EventID: "$e", TsMS: 1}}
	epoch := &restore.Epoch{RestoreEpoch: 0, Rev: 1}
	for name, c := range map[string]struct {
		op   string
		a    Answer
		fits bool
	}{
		"tail":             {OpCarryTail, Answer{OK: true, Tail: tail, Epoch: epoch}, true},
		"tail no epoch":    {OpCarryTail, Answer{OK: true, Tail: tail}, false},
		"tail no tail":     {OpCarryTail, Answer{OK: true, Epoch: epoch}, false},
		"check":            {OpCarryCheck, Answer{OK: true, Found: []bool{true}, Epoch: epoch}, true},
		"check with tail":  {OpCarryCheck, Answer{OK: true, Found: []bool{true}, Tail: tail, Epoch: epoch}, false},
		"place":            {OpCarryPlace, Answer{OK: true, Changed: []string{"roster.json written"}}, true},
		"place with epoch": {OpCarryPlace, Answer{OK: true, Epoch: epoch}, false},
		"place with notes": {OpCarryPlace, Answer{OK: true, Notes: []string{"n"}}, false},
		"restore check":    {OpRestoreCheck, Answer{OK: true, Found: []bool{true}, Epoch: epoch}, false},
	} {
		if err := answerFits(c.op, c.a); (err == nil) != c.fits {
			t.Errorf("%s: fits = %v", name, err)
		}
	}
	for name, a := range map[string]Answer{
		"tail room":    {OK: true, Tail: []carry.TailEntry{{Room: "elsewhere", EventID: "$e", TsMS: 1}}, Epoch: epoch},
		"tail twice":   {OK: true, Tail: []carry.TailEntry{tail[0], tail[0]}, Epoch: epoch},
		"tail time":    {OK: true, Tail: []carry.TailEntry{{Room: proto.PurposeMain, EventID: "$e", TsMS: -1}}, Epoch: epoch},
		"found empty":  {OK: true, Found: []bool{}, Epoch: epoch},
		"found export": {OK: true, Found: []bool{true}, Epoch: epoch, Export: &restore.Export{}},
		"tail handle":  {OK: true, Tail: tail, Epoch: epoch, Handle: sampleHandle},
	} {
		if _, err := EncodeAnswer(a); err == nil {
			t.Errorf("%s: a bad carry answer encoded", name)
		}
	}
}
