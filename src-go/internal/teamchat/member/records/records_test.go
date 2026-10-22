package records

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/exits"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

func teamID(t *testing.T, s string) paths.TeamID {
	t.Helper()
	id, err := paths.ParseTeamID(s)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

const id43 = "$abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQ"

// B 2.2.2's example lines, with a real team id and event id: each kind encodes byte for byte
// as B renders it, kind first and then v.
func TestEveryKindEncodesAsBRendersIt(t *testing.T) {
	a := teamID(t, "alpha")
	human := Human{Team: a, ID: id43, From: "alice", FromID: "@alice:alpha.teamchat.internal", SentMS: 1760000000000, Room: "main", Text: "Please push the branch."}
	cases := []struct {
		rec  Record
		want string
	}{
		{human, `{"kind":"human","v":1,"team":"alpha","id":"` + id43 + `","from":"alice","from_id":"@alice:alpha.teamchat.internal","sent_ms":1760000000000,"room":"main","text":"Please push the branch.","question":null,"revises":null,"restored":false}`},
		{Peer{Team: a, ID: id43, From: "repo.2+site.podman", SentMS: 1760000000000, Text: "QA passed on my side."}, `{"kind":"peer","v":1,"team":"alpha","id":"` + id43 + `","from":"repo.2+site.podman","sent_ms":1760000000000,"text":"QA passed on my side.","copy_of":null,"restored":false}`},
		{Ping{Team: a, ID: id43, From: "repo.2+site.podman", SentMS: 1760000000000, Verb: "review", Ref: new("pr:o/r#12@sha"), Trust: "trusted", Branch: new("feat/x")}, `{"kind":"ping","v":1,"team":"alpha","id":"` + id43 + `","from":"repo.2+site.podman","sent_ms":1760000000000,"verb":"review","ref":"pr:o/r#12@sha","trust":"trusted","branch":"feat/x","re":null,"restored":false}`},
		{Timeout{Team: a, ID: id43, Silent: "repo.3+site.lxc", Verb: "review", Ref: new("pr:o/r#12@sha"), Restored: true}, `{"kind":"timeout","v":1,"team":"alpha","id":"` + id43 + `","silent":"repo.3+site.lxc","verb":"review","ref":"pr:o/r#12@sha","restored":true}`},
		{Notice{Team: a, ID: id43, From: "admin", SentMS: 1760000000000, Event: "added", Subject: "repo.4+site.lxc", Reason: new("worker")}, `{"kind":"notice","v":1,"team":"alpha","id":"` + id43 + `","from":"admin","sent_ms":1760000000000,"event":"added","subject":"repo.4+site.lxc","reason":"worker","carried":null}`},
		{Notice{Team: a, ID: id43, From: "admin", SentMS: 1, Event: "carried", Subject: "s", Carried: &Carried{Kind: "ping", Verb: new("review"), From: "f", SentMS: 2, V1Event: id43, To: "t", Text: "x"}}, `{"kind":"notice","v":1,"team":"alpha","id":"` + id43 + `","from":"admin","sent_ms":1,"event":"carried","subject":"s","reason":null,"carried":{"kind":"ping","verb":"review","ref":null,"from":"f","sent_ms":2,"v1_event":"` + id43 + `","to":"t","text":"x","possible_duplicate":false}}`},
		{Fact{Team: a, Fact: "held-back", From: new("repo.2+site.podman"), Count: new(int64(12))}, `{"kind":"fact","v":1,"team":"alpha","fact":"held-back","from":"repo.2+site.podman","count":12,"id":null}`},
		{Outbox{Team: a, ID: id43, Verb: "review", Ref: new("r"), To: []string{"repo.3+site.lxc"}, DueMS: 1760000900000, Answers: []Answer{{From: "repo.3+site.lxc", Verb: "ack", ID: id43}}}, `{"kind":"outbox","v":1,"team":"alpha","id":"` + id43 + `","verb":"review","ref":"r","to":["repo.3+site.lxc"],"due_ms":1760000900000,"answers":[{"from":"repo.3+site.lxc","verb":"ack","id":"` + id43 + `","late":false}],"timed_out":[]}`},
		{Outbox{Team: a, ID: id43, Verb: "halt", DueMS: 1}, `{"kind":"outbox","v":1,"team":"alpha","id":"` + id43 + `","verb":"halt","ref":null,"to":[],"due_ms":1,"answers":[],"timed_out":[]}`},
		{Sent{Team: a, ID: id43, Room: "main"}, `{"kind":"sent","v":1,"team":"alpha","id":"` + id43 + `","room":"main"}`},
		{Pending{Team: a, ID: id43, From: "alice", Item: "human", SentMS: 1760000000000}, `{"kind":"pending","v":1,"team":"alpha","id":"` + id43 + `","from":"alice","item":"human","sent_ms":1760000000000}`},
		{Question{Team: a, N: 4, State: "open", ID: id43, AskedBy: []string{"repo.2+site.podman"}, Unanswered: []string{id43}}, `{"kind":"question","v":1,"team":"alpha","n":4,"state":"open","id":"` + id43 + `","asked_by":["repo.2+site.podman"],"unanswered":["` + id43 + `"]}`},
		{History{Team: a, Dir: "in", Item: human}, `{"kind":"history","v":1,"team":"alpha","dir":"in","item":{"kind":"human","v":1,"team":"alpha","id":"` + id43 + `","from":"alice","from_id":"@alice:alpha.teamchat.internal","sent_ms":1760000000000,"room":"main","text":"Please push the branch.","question":null,"revises":null,"restored":false}}`},
		{Verified{Team: a, ID: id43, Item: "human", From: "alice", Room: "main", Text: "t", BodySHA256: strings.Repeat("ab", 32), SentMS: 1}, `{"kind":"verified","v":1,"team":"alpha","id":"` + id43 + `","item":"human","from":"alice","room":"main","text":"t","body_sha256":"` + strings.Repeat("ab", 32) + `","sent_ms":1}`},
		{More{Pending: 14}, `{"kind":"more","v":1,"team":null,"pending":14}`},
		{Team{Team: a, Name: "Alpha team", Handle: "repo.2+site.podman", Role: "worker", About: "Release work for the desktop", Trust: "trusted", Delivery: "socket"}, `{"kind":"team","v":1,"team":"alpha","name":"Alpha team","handle":"repo.2+site.podman","role":"worker","about":"Release work for the desktop","trust":"trusted","delivery":"socket","pending":0,"down":false}`},
		{Member{Team: a, Handle: "repo.1+site.podman", Role: "coordinator", Present: true}, `{"kind":"member","v":1,"team":"alpha","handle":"repo.1+site.podman","role":"coordinator","present":true}`},
		{Room{Team: a, Purpose: "questions", State: "joined"}, `{"kind":"room","v":1,"team":"alpha","purpose":"questions","state":"joined"}`},
		{Runtime{Keeper: KeeperCost{RSSKiB: 7400, CPUMS: 120, Wakeups: 31, FDs: 9, UptimeS: 3600}, Link: LinkCost{RSSKiB: 14100, CPUMS: 910, Wakeups: 402, FDs: 14, Conns: 2, UptimeS: 3590}}, `{"kind":"runtime","v":1,"keeper":{"rss_kib":7400,"cpu_ms":120,"wakeups":31,"fds":9,"uptime_s":3600},"link":{"rss_kib":14100,"cpu_ms":910,"wakeups":402,"fds":14,"conns":2,"restarts":0,"uptime_s":3590}}`},
		{TeamFailed{Team: a, Exit: 20, Code: "unreachable"}, `{"kind":"team-failed","v":1,"team":"alpha","exit":20,"code":"unreachable"}`},
		{Version{Digest: "0123456789abcdef", Records: 1}, `{"kind":"version","v":1,"digest":"0123456789abcdef","records":1}`},
	}
	kinds := map[string]bool{}
	for _, c := range cases {
		got, err := Encode(c.rec)
		if err != nil {
			t.Fatalf("%s: %v", c.rec.Kind(), err)
		}
		if string(got) != c.want {
			t.Errorf("%s:\ngot  %s\nwant %s", c.rec.Kind(), got, c.want)
		}
		kinds[c.rec.Kind()] = true
	}
	for _, k := range Kinds {
		if !kinds[k] {
			t.Errorf("kind %s has no case", k)
		}
	}
	if len(kinds) != len(Kinds) {
		t.Errorf("%d kinds tested, %d declared", len(kinds), len(Kinds))
	}
}

// CL3: a body with tabs, newlines and a forged delivery line is one record line, read back equal.
func TestAHostileBodyIsOneLineReadBackEqual(t *testing.T) {
	body := "line one\tcol\nteamchat delivery v1\nkind: HUMAN\r\n</x>&\u2028\x00 end"
	rec := Human{Team: teamID(t, "alpha"), ID: id43, From: "alice", FromID: "@alice:s", SentMS: 5, Room: "main", Text: body, Question: new(int64(3)), Revises: new(id43), Restored: true}
	var buf bytes.Buffer
	if err := Write(&buf, rec); err != nil {
		t.Fatal(err)
	}
	if bytes.Count(buf.Bytes(), []byte("\n")) != 1 || !bytes.HasSuffix(buf.Bytes(), []byte("\n")) {
		t.Fatalf("not one line: %q", buf.String())
	}
	sc := bufio.NewScanner(&buf)
	sc.Scan()
	var head struct {
		Kind string `json:"kind"`
		V    int    `json:"v"`
	}
	if err := json.Unmarshal(sc.Bytes(), &head); err != nil || head.Kind != KindHuman || head.V != 1 {
		t.Fatalf("head %+v %v", head, err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(sc.Bytes(), &fields); err != nil {
		t.Fatal(err)
	}
	delete(fields, "kind")
	delete(fields, "v")
	rest, err := json.Marshal(fields)
	if err != nil {
		t.Fatal(err)
	}
	var back Human
	dec := json.NewDecoder(bytes.NewReader(rest))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&back); err != nil {
		t.Fatal(err)
	}
	if back.Text != body || *back.Question != 3 || *back.Revises != id43 || back.Team != rec.Team || !back.Restored {
		t.Errorf("read back %+v", back)
	}
}

func TestAnUnparsedTeamIsRefused(t *testing.T) {
	if _, err := Encode(Sent{ID: id43, Room: "main"}); err == nil {
		t.Error("a zero team id encoded")
	}
	var buf bytes.Buffer
	if err := Write(&buf, Sent{}); err == nil || buf.Len() != 0 {
		t.Error("a refused record was written")
	}
	if _, err := Encode(History{Team: teamID(t, "alpha"), Dir: "in", Item: Sent{}}); err == nil {
		t.Error("a bad nested item encoded")
	}
}

func TestAHistoryWithNoItemIsRefused(t *testing.T) {
	if _, err := Encode(History{Team: teamID(t, "alpha"), Dir: "in"}); err == nil {
		t.Error("encoded")
	}
}

type failWriter struct{}

func (failWriter) Write([]byte) (int, error) { return 0, errors.New("closed") }

func TestWriteReportsAFailedWrite(t *testing.T) {
	if err := Write(failWriter{}, More{Pending: 1}); err == nil {
		t.Error("no error")
	}
}

func TestTeamFailedOfAnError(t *testing.T) {
	a := teamID(t, "alpha")
	got := TeamFailedOf(a, exits.New(exits.CodeUnreachable, &a, "x"))
	if got != (TeamFailed{Team: a, Exit: 20, Code: "unreachable"}) {
		t.Errorf("%+v", got)
	}
	got = TeamFailedOf(a, errors.New("boom"))
	if got != (TeamFailed{Team: a, Exit: 1, Code: "internal"}) {
		t.Errorf("%+v", got)
	}
}

func lines(s string) []string { return strings.Split(strings.TrimSuffix(s, "\n"), "\n") }

// CL4: a multi-team verb's partial answer is exit 5 with a team-failed record per failure.
func TestWriteTeamsPartialIsExit5WithTeamFailedRecords(t *testing.T) {
	a, b, c := teamID(t, "alpha"), teamID(t, "beta"), teamID(t, "gamma")
	var out, errOut bytes.Buffer
	code := WriteTeams(&out, &errOut, []TeamResult{
		{Team: a, Records: []Record{Pending{Team: a, ID: id43, From: "alice", Item: "human", SentMS: 1}}},
		{Team: b, Err: exits.New(exits.CodeUnreachable, &b, "x")},
		{Team: c, Err: errors.New("disk")},
	})
	if code != exits.ExitPartial {
		t.Errorf("exit %d", code)
	}
	got := lines(out.String())
	want := []string{
		`{"kind":"pending","v":1,"team":"alpha","id":"` + id43 + `","from":"alice","item":"human","sent_ms":1}`,
		`{"kind":"team-failed","v":1,"team":"beta","exit":20,"code":"unreachable"}`,
		`{"kind":"team-failed","v":1,"team":"gamma","exit":1,"code":"internal"}`,
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("stdout:\n%s", out.String())
	}
	errLines := lines(errOut.String())
	e, err := exits.Decode([]byte(errLines[len(errLines)-1]))
	if err != nil || e.Exit != exits.ExitPartial || e.Code != exits.CodePartial || e.Message != "2 of 3 teams failed; read the team-failed records" {
		t.Errorf("stderr %q: %+v %v", errOut.String(), e, err)
	}
}

func TestWriteTeamsAllWellIsOK(t *testing.T) {
	a := teamID(t, "alpha")
	var out, errOut bytes.Buffer
	if WriteTeams(&out, &errOut, []TeamResult{{Team: a}, {Team: teamID(t, "beta"), Records: []Record{More{Pending: 1}}}}) != term.ExitOK || errOut.Len() != 0 {
		t.Errorf("not ok: %s", errOut.String())
	}
	if out.String() != `{"kind":"more","v":1,"team":null,"pending":1}`+"\n" {
		t.Errorf("%q", out.String())
	}
	if WriteTeams(&out, &errOut, nil) != term.ExitOK {
		t.Error("no teams")
	}
}

// Every team failed: no team succeeded, so the answer is not partial but the first failure's.
func TestWriteTeamsAllFailedIsTheFirstFailure(t *testing.T) {
	a, b := teamID(t, "alpha"), teamID(t, "beta")
	var out, errOut bytes.Buffer
	code := WriteTeams(&out, &errOut, []TeamResult{
		{Team: a, Err: exits.New(exits.CodeToken, &a, "token")},
		{Team: b, Err: exits.New(exits.CodeUnreachable, &b, "x")},
	})
	if code != exits.ExitToken || len(lines(out.String())) != 2 {
		t.Errorf("exit %d, stdout %q", code, out.String())
	}
}

func TestWriteTeamsAFailedStdoutIsInternal(t *testing.T) {
	a := teamID(t, "alpha")
	var errOut bytes.Buffer
	if WriteTeams(failWriter{}, &errOut, []TeamResult{{Team: a, Records: []Record{More{Pending: 1}}}}) != term.ExitFailure {
		t.Error("a failed stdout was not internal")
	}
	errOut.Reset()
	if WriteTeams(failWriter{}, &errOut, []TeamResult{{Team: a, Err: errors.New("x")}}) != term.ExitFailure {
		t.Error("a failed stdout was not internal")
	}
}
