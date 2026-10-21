package hooks

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/pingbus/pyjson"
)

// The validator's world: a team with two humans, two workers and a coordinator, an org with an
// excluded repository, a listed repository on one branch and another on any branch.
const (
	vServer = "alpha.example.org"
	vSelf   = "@proj.1+box.podman:" + vServer
	vCoord  = "@proj.2+box.podman:" + vServer
	vPeer   = "@proj.3+box.podman:" + vServer
	vAlice  = "@alice:" + vServer
	vBob    = "@bob:" + vServer
	vSHA    = "0123456789abcdef0123456789abcdef01234567"
	vID     = "$" + "E000000000000000000000000000000000000000001"
	vOther  = "$" + "E000000000000000000000000000000000000000002"
	vRecord = `{"v": 1, "team": "alpha", "humans": ["` + vAlice + `", "` + vBob + `"], "roles": {"` + vSelf +
		`": "worker", "` + vCoord + `": "orchestrator", "` + vPeer + `": "worker"}, "org": "org", "exclude": ["secret"], ` +
		`"repos": [{"repo": "org/proj", "branches": ["main"]}, {"repo": "other/any"}], "path_prefixes": ["docs/", "README.md"], ` +
		`"forge_api": "https://api.github.com"}`
)

func vEvent(sender, content string) string {
	return `{"type": "m.room.message", "event_id": "` + vID + `", "sender": "` + sender +
		`", "origin_server_ts": 5, "content": ` + content + `}`
}

func vPing(verb, to, ref, extra string) string {
	body := "[agent-bus] " + verb + " " + ref + " -> " + to
	refKey := `, "ref": "` + ref + `"`
	if ref == "-" {
		refKey = ""
	}
	return `{"msgtype": "m.notice", "body": "` + body + `", "m.mentions": {"user_ids": ["` + to +
		`"]}, "agent_bus.ping": {"v": 1, "verb": "` + verb + `", "to": ["` + to + `"]` + refKey + extra + `}}`
}

func vText(text string) string {
	to := vSelf
	return `{"msgtype": "m.notice", "body": "[agent-bus] text -> ` + to + `\n` + text + `", "m.mentions": {"user_ids": ["` + to +
		`"]}, "agent_bus.text": {"v": 1, "to": ["` + to + `"], "text": "` + text + `"}}`
}

func vHuman(body, extra string) string {
	return `{"msgtype": "m.text", "body": "` + body + `", "m.mentions": {"user_ids": ["` + vSelf + `"]}` + extra + `}`
}

type vCase struct {
	name, event, self, room string
	humanText               bool
}

// vCases is the corpus: each good event, then one change of it each.
func vCases() []vCase {
	commit := "commit:org/proj@" + vSHA
	review := vEvent(vCoord, vPing("review", vSelf, commit, ""))
	text := vEvent(vPeer, vText("hello there"))
	human := vEvent(vAlice, vHuman("please look", ""))
	edits := map[string][2]string{}
	for _, part := range []map[string][2]string{pingEdits(review, commit), textEdits(text), humanEdits(human)} {
		maps.Copy(edits, part)
	}
	return casesOf(edits)
}

func pingEdits(review, commit string) map[string][2]string {
	return map[string][2]string{
		"ping":                 {review, review},
		"ping not addressed":   {review, strings.ReplaceAll(review, vSelf, vPeer)},
		"ping to human":        {review, strings.ReplaceAll(review, vSelf, vAlice)},
		"ping v2":              {review, strings.Replace(review, `"v": 1`, `"v": 2`, 1)},
		"ping v true":          {review, strings.Replace(review, `"v": 1`, `"v": true`, 1)},
		"ping v float":         {review, strings.Replace(review, `"v": 1`, `"v": 1.0`, 1)},
		"ping edit":            {review, strings.Replace(review, `"msgtype"`, `"m.new_content": {}, "msgtype"`, 1)},
		"ping extra key":       {review, strings.Replace(review, `"msgtype"`, `"x": 1, "msgtype"`, 1)},
		"ping msgtype":         {review, strings.Replace(review, `"m.notice"`, `"m.text"`, 1)},
		"ping obj extra":       {review, strings.Replace(review, `"verb"`, `"x": "y", "verb"`, 1)},
		"ping no to":           {review, strings.Replace(review, `"to": ["`+vSelf+`"]`, `"too": []`, 1)},
		"ping verb":            {review, strings.ReplaceAll(review, "review", "explode")},
		"ping verb number":     {review, strings.Replace(review, `"verb": "review"`, `"verb": 3`, 1)},
		"ping to twice":        {review, strings.Replace(review, `"to": ["`+vSelf+`"]`, `"to": ["`+vSelf+`", "`+vSelf+`"]`, 1)},
		"ping to empty":        {review, strings.Replace(review, `"to": ["`+vSelf+`"]`, `"to": []`, 1)},
		"ping to number":       {review, strings.Replace(review, `"to": ["`+vSelf+`"]`, `"to": [1]`, 1)},
		"ping ref missing":     {review, strings.Replace(review, `, "ref": "`+commit+`"`, "", 1)},
		"ping ref bad":         {review, strings.ReplaceAll(review, commit, "commit:org/proj@abc")},
		"ping ref excluded":    {review, strings.ReplaceAll(review, "org/proj", "org/secret")},
		"ping ref org":         {review, strings.ReplaceAll(review, "org/proj", "org/else")},
		"ping ref external":    {review, strings.ReplaceAll(review, "org/proj", "stranger/x")},
		"ping ref .git":        {review, strings.ReplaceAll(review, "org/proj", "org/proj.git")},
		"ping body":            {review, strings.Replace(review, "[agent-bus] review", "[agent-bus]  review", 1)},
		"ping body number":     {review, strings.Replace(review, `"body": "[agent-bus] review `+commit+` -> `+vSelf+`"`, `"body": 7`, 1)},
		"ping mentions":        {review, strings.Replace(review, `"m.mentions": {"user_ids": ["`+vSelf+`"]}`, `"m.mentions": {"user_ids": ["`+vSelf+`"], "room": true}`, 1)},
		"ping from human":      {review, strings.Replace(review, `"sender": "`+vCoord, `"sender": "`+vBob, 1)},
		"ping from worker":     {review, strings.Replace(review, `"sender": "`+vCoord, `"sender": "`+vPeer, 1)},
		"ping from stranger":   {review, strings.Replace(review, `"sender": "`+vCoord, `"sender": "@x.1+y.vm:`+vServer, 1)},
		"ping sender number":   {review, strings.Replace(review, `"sender": "`+vCoord+`"`, `"sender": 4`, 1)},
		"ping from self":       {review, strings.Replace(review, `"sender": "`+vCoord, `"sender": "`+vSelf, 1)},
		"ping ts negative":     {review, strings.Replace(review, `"origin_server_ts": 5`, `"origin_server_ts": -1`, 1)},
		"ping ts float":        {review, strings.Replace(review, `"origin_server_ts": 5`, `"origin_server_ts": 5.0`, 1)},
		"ping ts bool":         {review, strings.Replace(review, `"origin_server_ts": 5`, `"origin_server_ts": true`, 1)},
		"ping ts huge":         {review, strings.Replace(review, `"origin_server_ts": 5`, `"origin_server_ts": 123456789012345678901234567890`, 1)},
		"ping state key":       {review, strings.Replace(review, `"type"`, `"state_key": "", "type"`, 1)},
		"ping redacted":        {review, strings.Replace(review, `"type"`, `"unsigned": {"redacted_because": {}}, "type"`, 1)},
		"ping unsigned plain":  {review, strings.Replace(review, `"type"`, `"unsigned": {"age": 1}, "type"`, 1)},
		"ping not a message":   {review, strings.Replace(review, `"m.room.message"`, `"m.room.member"`, 1)},
		"ping bad event id":    {review, strings.Replace(review, vID, "$short", 1)},
		"ping content list":    {review, vEvent(vCoord, `[]`)},
		"ping large":           {review, strings.Replace(review, `"msgtype"`, `"pad": "`+strings.Repeat("x", 4100)+`", "msgtype"`, 1)},
		"ping surrogate":       {review, strings.Replace(review, `"msgtype"`, `"pad": "\ud800", "msgtype"`, 1)},
		"ping NaN":             {review, strings.Replace(review, `"msgtype"`, `"pad": NaN, "msgtype"`, 1)},
		"ping unicode size":    {review, strings.Replace(review, `"msgtype"`, `"pad": "`+strings.Repeat("é", 1400)+`", "msgtype"`, 1)},
		"halt":                 {review, vEvent(vCoord, vPing("halt", vSelf, "-", ""))},
		"halt with ref":        {review, vEvent(vCoord, strings.Replace(vPing("halt", vSelf, "-", ""), "[agent-bus] halt -", "[agent-bus] halt "+commit, 1)+"")},
		"ack":                  {review, vEvent(vPeer, strings.Replace(vPing("ack", vSelf, "-", `, "re": "`+vOther+`"`), " -> "+vSelf, " -> "+vSelf+" re "+vOther, 1))},
		"ack no re":            {review, vEvent(vPeer, vPing("ack", vSelf, "-", ""))},
		"ack bad re":           {review, vEvent(vPeer, strings.Replace(vPing("ack", vSelf, "-", `, "re": "x"`), " -> "+vSelf, " -> "+vSelf+" re x", 1))},
		"fetch by worker":      {review, vEvent(vPeer, vPing("fetch", vSelf, commit, ""))},
		"fetch external":       {review, vEvent(vCoord, vPing("fetch", vSelf, "commit:stranger/x@"+vSHA, ""))},
		"sync external":        {review, vEvent(vCoord, vPing("sync", vSelf, "commit:stranger/x@"+vSHA, ""))},
		"run-qa path":          {review, vEvent(vCoord, vPing("run-qa", vSelf, "path:org/proj@"+vSHA+":docs/a.md", ""))},
		"review path ok":       {review, vEvent(vCoord, vPing("review", vSelf, "path:org/proj@"+vSHA+":docs/a.md", ""))},
		"review path readme":   {review, vEvent(vCoord, vPing("review", vSelf, "path:org/proj@"+vSHA+":README.md", ""))},
		"review path outside":  {review, vEvent(vCoord, vPing("review", vSelf, "path:org/proj@"+vSHA+":src/a.go", ""))},
		"review path dotdot":   {review, vEvent(vCoord, vPing("review", vSelf, "path:org/proj@"+vSHA+":docs/../a", ""))},
		"review path external": {review, vEvent(vCoord, vPing("review", vSelf, "path:stranger/x@"+vSHA+":src/a.go", ""))},
		"review pr":            {review, vEvent(vCoord, vPing("review", vSelf, "pr:org/proj#12@"+vSHA, ""))},
		"review issue":         {review, vEvent(vCoord, vPing("review", vSelf, "issue:org/proj#12", ""))},
		"done issue":           {review, vEvent(vPeer, vPing("done", vSelf, "issue:org/proj#12", ""))},
		"done to human":        {review, vEvent(vPeer, vPing("done", vAlice, "issue:org/proj#12", ""))},
		"nack no ref":          {review, vEvent(vPeer, strings.Replace(vPing("nack", vSelf, "-", `, "re": "`+vOther+`"`), " -> "+vSelf, " -> "+vSelf+" re "+vOther, 1))},
		"branch":               {review, vEvent(vCoord, strings.Replace(vPing("review", vSelf, commit, `, "branch": "main"`), " -> ", " on main -> ", 1))},
		"branch on pr":         {review, vEvent(vCoord, strings.Replace(vPing("review", vSelf, "pr:org/proj#1@"+vSHA, `, "branch": "main"`), " -> ", " on main -> ", 1))},
		"branch bad":           {review, vEvent(vCoord, strings.Replace(vPing("review", vSelf, commit, `, "branch": "a b"`), " -> ", " on a b -> ", 1))},
	}
}

func textEdits(text string) map[string][2]string {
	return map[string][2]string{
		"text":                  {text, text},
		"text not named":        {text, strings.ReplaceAll(text, vSelf, vCoord)},
		"text to human":         {text, strings.ReplaceAll(text, vSelf, vAlice)},
		"text human_text false": {text, text},
		"text and ping":         {text, strings.Replace(text, `"msgtype"`, `"agent_bus.ping": {}, "msgtype"`, 1)},
		"text v2":               {text, strings.Replace(text, `"v": 1`, `"v": 2`, 1)},
		"text v str":            {text, strings.Replace(text, `"v": 1`, `"v": "1"`, 1)},
		"text empty":            {text, vEvent(vPeer, vText(""))},
		"text surrogate":        {text, vEvent(vPeer, vText(`\ud800`))},
		"text large":            {text, vEvent(vPeer, vText(strings.Repeat("y", 4097)))},
		"text body":             {text, strings.Replace(text, `hello there", "m.mentions"`, `hello", "m.mentions"`, 1)},
		"text reply":            {text, strings.Replace(text, `"msgtype"`, `"m.relates_to": {"m.in_reply_to": {"event_id": "`+vOther+`"}}, "msgtype"`, 1)},
		"text reply bad":        {text, strings.Replace(text, `"msgtype"`, `"m.relates_to": {"m.in_reply_to": {"event_id": "x"}}, "msgtype"`, 1)},
		"text replace":          {text, strings.Replace(text, `"msgtype"`, `"m.relates_to": {"rel_type": "m.replace"}, "msgtype"`, 1)},
		"text new content":      {text, strings.Replace(text, `"msgtype"`, `"m.new_content": {}, "msgtype"`, 1)},
		"text from human key":   {text, strings.Replace(text, `"sender": "`+vPeer, `"sender": "`+vAlice, 1)},
		"text secret":           {text, vEvent(vPeer, vText("password=hunter2hunter2"))},
		"text mentions extra":   {text, strings.Replace(text, `"m.mentions": {"user_ids": ["`+vSelf+`"]}`, `"m.mentions": {"user_ids": ["`+vSelf+`"], "room": false}`, 1)},
		"text to list":          {text, strings.Replace(text, `"text": "hello there"}`, `"text": "hello there", "x": 1}`, 1)},
		"text coord copy":       {text, text},
	}
}

func humanEdits(human string) map[string][2]string {
	return map[string][2]string{
		"human":                   {human, human},
		"human not addressed":     {human, strings.Replace(human, `"user_ids": ["`+vSelf+`"]`, `"user_ids": ["`+vPeer+`"]`, 1)},
		"human room mention":      {human, strings.Replace(human, `"user_ids": ["`+vSelf+`"]`, `"room": true`, 1)},
		"human room mention one":  {human, strings.Replace(human, `"user_ids": ["`+vSelf+`"]`, `"room": 1`, 1)},
		"human mentions list":     {human, strings.Replace(human, `{"user_ids": ["`+vSelf+`"]}`, `[]`, 1)},
		"human notice":            {human, strings.Replace(human, `"m.text"`, `"m.notice"`, 1)},
		"human body number":       {human, strings.Replace(human, `"please look"`, `12`, 1)},
		"human large":             {human, vEvent(vAlice, vHuman(strings.Repeat("z", 16385), ""))},
		"human surrogate":         {human, vEvent(vAlice, vHuman(`\udc00`, ""))},
		"human edit":              {human, vEvent(vAlice, vHuman("x", `, "m.new_content": {}`))},
		"human replace":           {human, vEvent(vAlice, vHuman("x", `, "m.relates_to": {"rel_type": "m.replace"}`))},
		"human relates list":      {human, vEvent(vAlice, vHuman("x", `, "m.relates_to": []`))},
		"human reply":             {human, vEvent(vAlice, vHuman(`> quoted\n\nanswer`, `, "m.relates_to": {"m.in_reply_to": {"event_id": "`+vOther+`"}}`))},
		"human reply empty":       {human, vEvent(vAlice, vHuman(`> quoted\n`, `, "m.relates_to": {"m.in_reply_to": {}}`))},
		"human no text":           {human, human},
		"human sender list":       {human, strings.Replace(human, `"sender": "`+vAlice+`"`, `"sender": ["`+vAlice+`"]`, 1)},
		"questions":               {human, human},
		"questions not addressed": {human, strings.Replace(human, `"user_ids": ["`+vSelf+`"]`, `"user_ids": []`, 1)},
		"questions edit":          {human, vEvent(vAlice, vHuman("x", `, "m.new_content": {"msgtype": "m.text", "body": "new"}, "m.relates_to": {"rel_type": "m.replace", "event_id": "`+vOther+`"}`))},
		"questions edit bad":      {human, vEvent(vAlice, vHuman("x", `, "m.new_content": {"msgtype": "m.notice", "body": "new"}, "m.relates_to": {"rel_type": "m.replace", "event_id": "`+vOther+`"}`))},
		"questions edit no id":    {human, vEvent(vAlice, vHuman("x", `, "m.new_content": {"msgtype": "m.text"}, "m.relates_to": {"rel_type": "m.replace"}`))},
		"questions reply":         {human, vEvent(vAlice, vHuman(`> q\nanswer`, `, "m.relates_to": {"m.in_reply_to": {}}`))},
		"questions empty":         {human, vEvent(vAlice, vHuman(``, ``))},
		"questions agent":         {human, strings.Replace(human, `"sender": "`+vAlice, `"sender": "`+vPeer, 1)},
		"questions coordinator":   {human, strings.Replace(human, `"sender": "`+vAlice, `"sender": "`+vCoord, 1)},
		"questions sender list":   {human, strings.Replace(human, `"sender": "`+vAlice+`"`, `"sender": ["`+vAlice+`"]`, 1)},
		"questions sender object": {human, strings.Replace(human, `"sender": "`+vAlice+`"`, `"sender": {}`, 1)},
		"questions surrogate":     {human, vEvent(vAlice, vHuman(`\ud800`, ""))},
		"questions human_text":    {human, human},
		"questions as worker":     {human, human},
		"not an object":           {human, `[]`},
	}
}

func casesOf(edits map[string][2]string) []vCase {
	cases := make([]vCase, 0, len(edits))
	for _, name := range slices.Sorted(maps.Keys(edits)) {
		pair := edits[name]
		c := vCase{name: name, event: pair[1], self: vSelf, room: roomMain, humanText: true}
		switch name {
		case "text human_text false", "human no text", "questions human_text":
			c.humanText = false
		case "text coord copy":
			c.self = vCoord
			c.event = strings.ReplaceAll(pair[1], vSelf, vCoord)
		}
		if strings.HasPrefix(name, "questions") {
			c.room = purposeQuestions
			if name != "questions as worker" {
				c.self = vCoord
				c.event = strings.ReplaceAll(c.event, `"user_ids": ["`+vSelf+`"]`, `"user_ids": ["`+vCoord+`"]`)
			}
		}
		cases = append(cases, c)
	}
	return cases
}

type vResult struct {
	Kind   string  `json:"kind"`
	Reason *string `json:"reason"`
	Class  string  `json:"class"`
	Error  string  `json:"error"`
}

func vRecordOf(t *testing.T) teamRecord {
	t.Helper()
	content, err := pyjson.Decode(vRecord, pyjson.Options{})
	if err != nil {
		t.Fatal(err)
	}
	record, err := parseTeamRecord(content, vServer, "alpha")
	if err != nil {
		t.Fatal(err)
	}
	return record
}

// goOutcome is the Go validator's answer in the oracle's terms.
func goOutcome(t *testing.T, record teamRecord, c vCase) vResult {
	t.Helper()
	event, err := pyjson.Decode(c.event, pyjson.Options{})
	if err != nil {
		t.Fatalf("%s: %v", c.name, err)
	}
	got, err := validateEvent(event, record, vServer, c.self, c.humanText, c.room)
	if err != nil {
		if !errors.As(err, new(*internalError)) {
			t.Errorf("%s: %v is not an internal error", c.name, err)
		}
		return vResult{Error: "raised"}
	}
	r := vResult{Kind: got.kind, Class: got.class}
	if got.reason != "" {
		r.Reason = &got.reason
	}
	return r
}

// H28: the Go validator agrees with v1's validate_event on every case of the corpus: the
// outcome, its reason and the class counted, and an exception where v1 raises one.
func TestValidateEventAgreesWithThePython(t *testing.T) {
	record := vRecordOf(t)
	cases := vCases()
	type request struct {
		Record string           `json:"record"`
		Server string           `json:"server"`
		Team   string           `json:"team"`
		Cases  []map[string]any `json:"cases"`
	}
	req := request{Record: vRecord, Server: vServer, Team: "alpha"}
	for _, c := range cases {
		req.Cases = append(req.Cases, map[string]any{"event": c.event, "self": c.self, "human_text": c.humanText, "room": c.room})
	}
	stdin, err := json.Marshal(req)
	if err != nil {
		t.Fatal(err)
	}
	script, err := filepath.Abs(filepath.Join("testdata", "validate_events.py"))
	if err != nil {
		t.Fatal(err)
	}
	cmd := execx.Cmd{Name: "python3", Args: []string{"-I", script, checkoutOf(t)}, Stdin: bytes.NewReader(stdin)}
	res, err := (execx.OS{}).Run(context.Background(), cmd)
	if err != nil {
		t.Fatalf("the Python validator: %v", err)
	}
	lines := strings.Split(strings.TrimSuffix(string(res.Stdout), "\n"), "\n")
	if len(lines) != len(cases) {
		t.Fatalf("%d answers for %d cases", len(lines), len(cases))
	}
	accepted := 0
	for i, c := range cases {
		var want vResult
		if err := json.Unmarshal([]byte(lines[i]), &want); err != nil {
			t.Fatal(err)
		}
		if want.Error != "" {
			want = vResult{Error: "raised"}
		}
		if got := goOutcome(t, record, c); !sameResult(got, want) {
			t.Errorf("%s: Go %s, Python %s", c.name, describe(got), describe(want))
		}
		if want.Kind == outcomeAccept {
			accepted++
		}
	}
	if accepted < 10 {
		t.Errorf("only %d cases accepted: the corpus does not reach the accepting paths", accepted)
	}
}

func sameResult(a, b vResult) bool {
	return a.Kind == b.Kind && a.Class == b.Class && a.Error == b.Error && (a.Reason == nil) == (b.Reason == nil) &&
		(a.Reason == nil || *a.Reason == *b.Reason)
}

func describe(r vResult) string {
	if r.Error != "" {
		return "raised"
	}
	reason := "-"
	if r.Reason != nil {
		reason = *r.Reason
	}
	return r.Kind + "/" + reason + "/" + r.Class
}

// The class each accepted path counts, without the oracle.
func TestValidateEventClasses(t *testing.T) {
	record := vRecordOf(t)
	byName := map[string]vCase{}
	for _, c := range vCases() {
		byName[c.name] = c
	}
	for name, class := range map[string]string{"ping": "ping", "text": "peer", "human": "human", "questions": "human"} {
		if got := goOutcome(t, record, byName[name]); got.Kind != outcomeAccept || got.Class != class {
			t.Errorf("%s: %s, want accept as %s", name, describe(got), class)
		}
	}
}

// checkoutOf walks up from the package directory to the checkout that holds helpers/pingbus.
func checkoutOf(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		_, statErr := os.Stat(filepath.Join(dir, "helpers", "pingbus", "protocol.py"))
		if statErr == nil {
			return dir
		}
		if !errors.Is(statErr, fs.ErrNotExist) {
			t.Fatal(statErr)
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("no checkout with helpers/pingbus above the package")
		}
		dir = parent
	}
}
