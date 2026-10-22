package admin

import (
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/apply"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/health"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/status"
)

var sampleRoom = "!" + strings.Repeat("m", 43)

func sampleView() *health.View {
	rooms := map[string]string{"main": "join", "reports": "invite"}
	return &health.View{
		Members: []status.Member{
			{Handle: sampleHandle, Kind: "podman", Role: "worker", State: "active", Rooms: rooms},
			{Handle: "myrepo.4+desk.lxc", Kind: status.KindLXC, Role: "coordinator", State: "parked", Rooms: map[string]string{}},
		},
		Humans:   []status.Human{{Name: "alice", State: "active", Rooms: rooms, Devices: 2, LastSeenMS: 7}},
		Problems: []status.Problem{{What: "the main room drifted", Fix: "ccy-teams apply alpha"}},
	}
}

const sampleViewWire = `"view":{"members":[` +
	`{"handle":"` + sampleHandle + `","kind":"podman","role":"worker","state":"active","rooms":{"main":"join","reports":"invite"}},` +
	`{"handle":"myrepo.4+desk.lxc","kind":"lxc","role":"coordinator","state":"parked","rooms":{}}],` +
	`"humans":[{"name":"alice","state":"active","rooms":{"main":"join","reports":"invite"},"devices":2,"last_seen_ms":7}],` +
	`"problems":[{"what":"the main room drifted","fix":"ccy-teams apply alpha"}]}`

// TestAnswerCarriesAViewANoticeAndTheMainRoom is CO1, CO2 and BU1 on the wire.
func TestAnswerCarriesAViewANoticeAndTheMainRoom(t *testing.T) {
	cases := map[string]struct {
		answer Answer
		wire   string
	}{
		"view": {Answer{OK: true, View: sampleView()}, `{"v":1,"ok":true,"changed":[],` + sampleViewWire + `}`},
		"empty view": {
			Answer{OK: true, View: &health.View{}},
			`{"v":1,"ok":true,"changed":[],"view":{"members":[],"humans":[],"problems":[]}}`,
		},
		"posted":     {Answer{OK: true, Notice: &health.Notice{Posted: true}}, `{"v":1,"ok":true,"changed":[],"notice":{"posted":true,"why":""}}`},
		"not posted": {Answer{OK: true, Notice: &health.Notice{Why: "the member has no token copy"}}, `{"v":1,"ok":true,"changed":[],"notice":{"posted":false,"why":"the member has no token copy"}}`},
	}
	for name, c := range cases {
		raw, err := EncodeAnswer(c.answer)
		if err != nil || string(raw) != c.wire {
			t.Errorf("%s: encode = %s, %v\nwant %s", name, raw, err, c.wire)
			continue
		}
		back, err := DecodeAnswer(raw)
		if err != nil {
			t.Errorf("%s: decode: %v", name, err)
			continue
		}
		again, err := EncodeAnswer(back)
		if err != nil || string(again) != c.wire {
			t.Errorf("%s: again = %s, %v", name, again, err)
		}
	}
}

// TestEncodeAnswerRefusesABadViewOrNotice is CO3's value rules: what the status record cannot
// hold, a notice that contradicts itself, and the main room without a credential or with one.
func TestEncodeAnswerRefusesABadViewOrNotice(t *testing.T) {
	cred := &apply.Credential{Kind: apply.CredToken, Secret: "s"}
	with := func(change func(*health.View)) *health.View {
		v := sampleView()
		change(v)
		return v
	}
	online := false
	for name, a := range map[string]Answer{
		"membership word":      {OK: true, View: with(func(v *health.View) { v.Members[0].Rooms = map[string]string{"main": "joined"} })},
		"room purpose":         {OK: true, View: with(func(v *health.View) { v.Humans[0].Rooms = map[string]string{"lobby": "join"} })},
		"kind":                 {OK: true, View: with(func(v *health.View) { v.Members[0].Kind = "Pod Man" })},
		"role":                 {OK: true, View: with(func(v *health.View) { v.Members[0].Role = "admin" })},
		"member state":         {OK: true, View: with(func(v *health.View) { v.Members[0].State = "gone" })},
		"handle":               {OK: true, View: with(func(v *health.View) { v.Members[0].Handle = "x y" })},
		"online":               {OK: true, View: with(func(v *health.View) { v.Members[1].Online = &online })},
		"no rooms":             {OK: true, View: with(func(v *health.View) { v.Members[0].Rooms = nil })},
		"human name":           {OK: true, View: with(func(v *health.View) { v.Humans[0].Name = "Alice!" })},
		"devices":              {OK: true, View: with(func(v *health.View) { v.Humans[0].Devices = -1 })},
		"problem lines":        {OK: true, View: with(func(v *health.View) { v.Problems[0].Fix = "a\nb" })},
		"posted with why":      {OK: true, Notice: &health.Notice{Posted: true, Why: "x"}},
		"not posted, no why":   {OK: true, Notice: &health.Notice{}},
		"why two lines":        {OK: true, Notice: &health.Notice{Why: "a\nb"}},
		"view and notice":      {OK: true, View: &health.View{}, Notice: &health.Notice{Posted: true}},
		"view and handle":      {OK: true, View: &health.View{}, Handle: sampleHandle},
		"notice and notes":     {OK: true, Notice: &health.Notice{Posted: true}, Notes: []string{"n"}},
		"view on a failure":    {View: &health.View{}, Error: &AnswerError{Class: ClassFailed, Message: "x"}},
		"notice on a failure":  {Notice: &health.Notice{Posted: true}, Error: &AnswerError{Class: ClassFailed, Message: "x"}},
		"room on a failure":    {MainRoom: sampleRoom, Error: &AnswerError{Class: ClassFailed, Message: "x"}},
		"room, no credential":  {OK: true, MainRoom: sampleRoom},
		"credential, no room":  {OK: true, Handle: sampleHandle, Credential: cred},
		"room not a room ID":   {OK: true, Handle: sampleHandle, Credential: cred, MainRoom: "#main:alpha"},
		"view with credential": {OK: true, View: &health.View{}, Credential: cred, MainRoom: sampleRoom},
	} {
		if _, err := EncodeAnswer(a); err == nil {
			t.Errorf("%s: encoded", name)
		}
	}
}

// TestDecodeAnswerReadsAViewStrictly is CO3: exact keys at every level, the record's own
// values, and nothing a failure or another field cannot carry.
func TestDecodeAnswerReadsAViewStrictly(t *testing.T) {
	ok := `{"v":1,"ok":true,"changed":[],`
	member := `{"handle":"` + sampleHandle + `","kind":"podman","role":"worker","state":"active","rooms":{}}`
	view := func(members, humans, problems string) string {
		return ok + `"view":{"members":[` + members + `],"humans":[` + humans + `],"problems":[` + problems + `]}}`
	}
	cases := map[string]string{
		"view on a failure":     `{"v":1,"ok":false,"view":{"members":[],"humans":[],"problems":[]},"error":{"class":"failed","message":"x"}}`,
		"notice on a failure":   `{"v":1,"ok":false,"notice":{"posted":true,"why":""},"error":{"class":"failed","message":"x"}}`,
		"room on a failure":     `{"v":1,"ok":false,"main_room":"` + sampleRoom + `","error":{"class":"failed","message":"x"}}`,
		"view extra key":        ok + `"view":{"members":[],"humans":[],"problems":[],"x":[]}}`,
		"view missing key":      ok + `"view":{"members":[],"humans":[]}}`,
		"view not an object":    ok + `"view":[]}`,
		"members not an array":  ok + `"view":{"members":{},"humans":[],"problems":[]}}`,
		"member online":         view(`{"handle":"`+sampleHandle+`","kind":"podman","role":"worker","state":"active","rooms":{},"online":true}`, "", ""),
		"member key case":       view(`{"Handle":"`+sampleHandle+`","kind":"podman","role":"worker","state":"active","rooms":{}}`, "", ""),
		"member missing rooms":  view(`{"handle":"`+sampleHandle+`","kind":"podman","role":"worker","state":"active"}`, "", ""),
		"member not an object":  view(`"`+sampleHandle+`"`, "", ""),
		"member rooms a list":   view(`{"handle":"`+sampleHandle+`","kind":"podman","role":"worker","state":"active","rooms":[]}`, "", ""),
		"member membership":     view(`{"handle":"`+sampleHandle+`","kind":"podman","role":"worker","state":"active","rooms":{"main":"joined"}}`, "", ""),
		"member room value":     view(`{"handle":"`+sampleHandle+`","kind":"podman","role":"worker","state":"active","rooms":{"main":1}}`, "", ""),
		"human extra key":       view(member, `{"name":"alice","state":"active","rooms":{},"devices":0,"last_seen_ms":0,"x":1}`, ""),
		"human devices string":  view(member, `{"name":"alice","state":"active","rooms":{},"devices":"0","last_seen_ms":0}`, ""),
		"problem extra key":     view(member, "", `{"what":"a","fix":"b","x":"c"}`),
		"problem not one line":  view(member, "", `{"what":"a b","fix":"b"}`),
		"notice extra key":      ok + `"notice":{"posted":true,"why":"","x":1}}`,
		"notice no why":         ok + `"notice":{"posted":false}}`,
		"notice posted string":  ok + `"notice":{"posted":"yes","why":"x"}}`,
		"notice why number":     ok + `"notice":{"posted":false,"why":1}}`,
		"notice why two lines":  ok + `"notice":{"posted":false,"why":"a\nb"}}`,
		"room number":           ok + `"handle":"` + sampleHandle + `","credential":{"kind":"token","secret":"s"},"main_room":1}`,
		"room, no credential":   ok + `"main_room":"` + sampleRoom + `"}`,
		"credential, no room":   ok + `"handle":"` + sampleHandle + `","credential":{"kind":"token","secret":"s"}}`,
		"view and notice":       ok + `"view":{"members":[],"humans":[],"problems":[]},"notice":{"posted":true,"why":""}}`,
		"view and a credential": ok + `"view":{"members":[],"humans":[],"problems":[]},"handle":"` + sampleHandle + `","credential":{"kind":"token","secret":"s"},"main_room":"` + sampleRoom + `"}`,
	}
	for name, raw := range cases {
		if _, err := DecodeAnswer([]byte(raw)); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	if _, err := DecodeAnswer([]byte(view(member, "", ""))); err != nil {
		t.Fatalf("the well-formed view: %v", err)
	}
}

// TestAnswerFitsItsOp is CO3's last rule: a view answers team.view only and a notice
// member.disconnected only, each always; a failure fits any op.
func TestAnswerFitsItsOp(t *testing.T) {
	view, notice := Answer{OK: true, View: &health.View{}}, Answer{OK: true, Notice: &health.Notice{Posted: true}}
	plain := Answer{OK: true}
	failed := Answer{Error: &AnswerError{Class: ClassFailed, Message: "x"}}
	cases := []struct {
		op   string
		a    Answer
		fits bool
	}{
		{OpTeamView, view, true},
		{OpTeamView, plain, false},
		{OpTeamView, notice, false},
		{OpTeamView, failed, true},
		{OpMemberDisconnected, notice, true},
		{OpMemberDisconnected, plain, false},
		{OpMemberDisconnected, view, false},
		{OpMemberDisconnected, failed, true},
		{apply.OpMemberPark, plain, true},
		{apply.OpMemberPark, view, false},
		{apply.OpMemberPark, notice, false},
		{apply.OpTeamApply, view, false},
	}
	for _, c := range cases {
		if err := answerFits(c.op, c.a); (err == nil) != c.fits {
			t.Errorf("%s %+v: %v", c.op, c.a, err)
		}
	}
}
