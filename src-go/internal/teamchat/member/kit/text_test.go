package kit_test

import (
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/cli"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/client"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/exits"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/hooktext"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/kit"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/records"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/state"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/proto"
)

// maxSkill bounds the skill: it is loaded into every member's context (B 2.12, kept short).
const maxSkill = 8 << 10

// shippedSkill is SKILL.md as a kit holds it.
func shippedSkill(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "kit")
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	k, err := paths.KitAt(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err = kit.Assemble(dir, k, strings.NewReader("bin")); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(kit.Skill(k))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// verbSlot stands for <VERB>, the one placeholder the agent fills: a command holding it is
// parsed once per answer verb.
const verbSlot = "\x00VERB\x00"

// fills are the placeholders an agent-facing text may hold, each with a value of its slot.
var fills = map[string]string{
	"'<id>'": "'$" + strings.Repeat("h", 43) + "'",
	"<id>":   "$" + strings.Repeat("h", 43),
	"<team>": "myteam",
	"<name>": "myrepo.2",
	"<verb>": "review",
	"<ref>":  "commit:example-org/myrepo@" + strings.Repeat("0", 40),
	"Q<n>":   "Q4",
	"<VERB>": verbSlot,
}

var placeholderRE = regexp.MustCompile(`'<[a-zA-Z-]+>'|Q<n>|<[a-zA-Z-]+>`)

// fill puts each placeholder's value in text; a placeholder it does not know fails.
func fill(t *testing.T, where, text string) string {
	t.Helper()
	return placeholderRE.ReplaceAllStringFunc(text, func(p string) string {
		v, ok := fills[p]
		if !ok {
			t.Errorf("%s: unknown placeholder %s", where, p)
			return p
		}
		return v
	})
}

// answerVerbs are A 8.2's answer verbs, which a reply: line's <VERB> takes.
func answerVerbs() []string {
	var out []string
	for v := range proto.Verbs {
		if proto.IsAnswerVerb(v) {
			out = append(out, v)
		}
	}
	slices.Sort(out)
	return out
}

// expand is a filled command once per answer verb when it holds <VERB>, else itself.
func expand(cmd string) []string {
	if !strings.Contains(cmd, verbSlot) {
		return []string{cmd}
	}
	var out []string
	for _, v := range answerVerbs() {
		out = append(out, strings.ReplaceAll(cmd, verbSlot, v))
	}
	return out
}

// agentVerbs are B 2.2.1's agent verbs by name.
func agentVerbs() map[string]cli.Verb {
	agent := map[string]cli.Verb{}
	for _, v := range cli.Verbs() {
		if v.Agent {
			agent[v.Name] = v
		}
	}
	return agent
}

// lifecycleWords are the first words of the verbs that are not for agents.
func lifecycleWords() map[string]bool {
	words := map[string]bool{}
	for _, v := range cli.Verbs() {
		if !v.Agent {
			words[strings.Fields(v.Name)[0]] = true
		}
	}
	return words
}

// prose are the words that follow "teamchat " where it names the product, not a command.
var prose = map[string]bool{
	"delivery": true, "deliveries": true, "sets": true, "is": true, "has": true, "verbs": true,
	"session": true, "brings": true, "for": true, "command": true, "member": true,
}

var (
	teamchatRE = regexp.MustCompile(`(^|[^A-Za-z0-9_./-])teamchat ([a-z][a-z-]*)`)
	wordRE     = regexp.MustCompile(`^[a-z]+$`)
	flagRE     = regexp.MustCompile(`--[a-z][a-z-]*`)
)

// unquote is a shell word's value: a single-quoted word without its quotes.
func unquote(w string) string {
	if len(w) >= 2 && w[0] == '\'' && w[len(w)-1] == '\'' {
		return w[1 : len(w)-1]
	}
	return w
}

// cut is a word without the punctuation that ends a clause, and whether it had any.
func cut(w string) (string, bool) {
	trimmed := strings.TrimRight(w, ",.;:)")
	return trimmed, trimmed != w
}

// found is one teamchat command in a text: its arguments after the program name.
type found struct{ args []string }

func (f found) String() string { return "teamchat " + strings.Join(f.args, " ") }

// scan finds every teamchat command in prose, and what is wrong in it. The verb's own table
// bounds a command: flags, the value of a flag that takes one, and the verb's positional
// arguments (an optional one only when it is not a plain word, or is a verb's name); it ends at
// a word that is none of these or at punctuation that ends a clause. "teamchat" followed by a
// word that is no verb must be one of the prose words; a lifecycle verb in agent-facing text
// is wrong.
func scan(text string) ([]found, []string) {
	agent, lifecycle := agentVerbs(), lifecycleWords()
	matches := teamchatRE.FindAllStringSubmatchIndex(text, -1)
	out := make([]found, 0, len(matches))
	var wrong []string
	for _, m := range matches {
		rest := text[m[4]:]
		if i := strings.IndexByte(rest, '\n'); i >= 0 {
			rest = rest[:i]
		}
		words := strings.Fields(rest)
		name, ended := cut(words[0])
		verb, ok := agent[name]
		switch {
		case ok:
		case lifecycle[name]:
			wrong = append(wrong, "the lifecycle verb "+name)
			continue
		case prose[name]:
			continue
		default:
			wrong = append(wrong, "teamchat "+name+", no verb and no known prose word")
			continue
		}
		args := []string{name}
		if !ended {
			args = append(args, extent(verb, words[1:], agent)...)
		}
		out = append(out, found{args: args})
	}
	return out, wrong
}

// extent is the words after a verb that belong to its command, unquoted.
func extent(verb cli.Verb, words []string, agent map[string]cli.Verb) []string {
	var args []string
	positional := 0
	for i := 0; i < len(words); i++ {
		w, end := cut(words[i])
		w = unquote(w)
		switch {
		case strings.HasPrefix(w, "--"):
			args = append(args, w)
			if takesValue(verb, w) && !end && i+1 < len(words) {
				i++
				v, e := cut(words[i])
				args = append(args, unquote(v))
				end = e
			}
		case positional < len(verb.Args) && (!verb.Args[positional].Optional || !wordRE.MatchString(w) || agent[w].Name != ""):
			args = append(args, w)
			positional++
		default:
			return args
		}
		if end {
			return args
		}
	}
	return args
}

func takesValue(v cli.Verb, flag string) bool {
	for _, f := range v.Flags {
		if "--"+f.Name == flag {
			return f.Value != ""
		}
	}
	return false
}

// scanned is scan with its findings reported against where.
func scanned(t *testing.T, where, text string) []found {
	t.Helper()
	out, wrong := scan(text)
	for _, w := range wrong {
		t.Errorf("%s: %s", where, w)
	}
	return out
}

// parses checks a command parses with CL2's own parser to an agent verb.
func parses(t *testing.T, where string, args []string) {
	t.Helper()
	inv, err := cli.Parse(args)
	switch {
	case err != nil:
		t.Errorf("%s: teamchat %s: %v", where, strings.Join(args, " "), err)
	case !inv.Verb.Agent:
		t.Errorf("%s: teamchat %s is not an agent verb", where, strings.Join(args, " "))
	}
}

// agentTexts are the binary's agent-facing texts besides the skill: A 13's corpus (every kind's
// entry, its authority sentence and reply: line, the trailing lines and MEMBERSHIP), the frame
// text, every hook template, and the client's 69 messages for every link-down reading.
func agentTexts(t *testing.T) map[string]string {
	t.Helper()
	texts := map[string]string{"proto.FrameText": proto.FrameText}
	corpus, err := filepath.Glob("../../proto/testdata/envelope/*.txt")
	if err != nil {
		t.Fatal(err)
	}
	if len(corpus) < 20 {
		t.Fatalf("the corpus: %d files", len(corpus))
	}
	for _, path := range corpus {
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			t.Fatal(readErr)
		}
		texts[filepath.Base(path)] = string(data)
	}
	for i, s := range hooktext.All() {
		texts["hooktext.All "+strconv.Itoa(i)] = s
	}
	run, err := client.Runtime(func(string) string { return "" })
	if err == nil {
		t.Fatalf("no TEAMCHAT_RUNTIME resolved to %v", run)
	}
	texts["client no-session"] = err.Error()
	for i, d := range []client.Down{
		{State: client.DownRestarting, NextMS: 5_000},
		{State: client.DownStarting},
		{State: client.DownFailed, Kind: state.KindCCY, Session: "work-1"},
		{State: client.DownFailed, Kind: state.KindLXC, Session: "work-1"},
		{State: client.DownFailed, Kind: state.KindHost},
	} {
		texts["client down "+strconv.Itoa(i)] = d.Error(nil, time.Second).Message
	}
	return texts
}

// PK2, B 2.12: every teamchat command in every agent-facing text parses with CL2's parser to an
// agent verb, its slots filled; a reply: line's <VERB> once with each answer verb.
func TestEveryAgentTextCommandParses(t *testing.T) {
	commands, verbLines := 0, 0
	for where, text := range agentTexts(t) {
		for _, f := range scanned(t, where, fill(t, where, text)) {
			cmd := strings.Join(f.args, " ")
			if strings.Contains(cmd, verbSlot) {
				verbLines++
			}
			for _, c := range expand(cmd) {
				parses(t, where, strings.Fields(c))
				commands++
			}
		}
	}
	if commands < 40 || verbLines == 0 {
		t.Errorf("%d commands, %d reply: lines with <VERB>: the scan found too little", commands, verbLines)
	}
	if !slices.Equal(answerVerbs(), []string{"ack", "blocked", "done", "nack"}) {
		t.Errorf("answer verbs %v", answerVerbs())
	}
}

// The scan and the parser refuse what they must, so the tests here can fail.
func TestTheTextCheckCanFail(t *testing.T) {
	for _, bad := range [][]string{
		{"reply", "$" + strings.Repeat("h", 43), "--verb", "review"},
		{"thread", "Qx"},
		{"recv", "--to", "x"},
		{"hook", "stop"},
	} {
		if inv, err := cli.Parse(bad); err == nil && inv.Verb.Agent {
			t.Errorf("teamchat %v parsed as an agent command", bad)
		}
	}
	for text, want := range map[string]struct {
		cmds  []string
		wrong string
	}{
		"run teamchat receive now":                                             {nil, "teamchat receive, no verb and no known prose word"},
		"run teamchat link --run x":                                            {nil, "the lifecycle verb link"},
		"teamchat delivery 1: a teamchat is here":                              {nil, ""},
		"remind them with teamchat thread Q4 --ask, or go on.":                 {[]string{"teamchat thread Q4 --ask"}, ""},
		"teamchat help lists the verbs; teamchat help exit-codes prints them":  {[]string{"teamchat help", "teamchat help exit-codes"}, ""},
		"run teamchat verify '$abc' with its id":                               {[]string{"teamchat verify $abc"}, ""},
		"see teamchat status --team myteam and tell a human":                   {[]string{"teamchat status --team myteam"}, ""},
		"Answer with teamchat reply '$x' --verb ack and do nothing else.":      {[]string{"teamchat reply $x --verb ack"}, ""},
		"teamchat ping review commit:o/r@1 --to a,b --branch feat/x, then go.": {[]string{"teamchat ping review commit:o/r@1 --to a,b --branch feat/x"}, ""},
	} {
		got, wrong := scan(text)
		cmds := make([]string, 0, len(got))
		for _, f := range got {
			cmds = append(cmds, f.String())
		}
		if !slices.Equal(cmds, want.cmds) || strings.Join(wrong, "; ") != want.wrong {
			t.Errorf("%q: %v %v", text, cmds, wrong)
		}
	}
}

// backtickRE is a code span of the skill.
var backtickRE = regexp.MustCompile("`([^`]*)`")

// PK2: SKILL.md's frontmatter names the skill, it is short and printable ASCII, and its eight
// points are in B 2.12's order.
func TestTheSkillIsShortAndInOrder(t *testing.T) {
	skill := shippedSkill(t)
	if len(skill) > maxSkill {
		t.Errorf("the skill is %d bytes, over %d", len(skill), maxSkill)
	}
	for i, r := range skill {
		if r > 0x7e || (r < 0x20 && r != '\n') {
			t.Fatalf("byte %d: %q is not printable ASCII", i, r)
		}
	}
	front, ok := strings.CutPrefix(skill, "---\nname: "+paths.Product+"\ndescription: ")
	end := strings.Index(front, "\n---\n")
	if !ok || end < 40 {
		t.Fatalf("frontmatter: %q", skill[:min(len(skill), 200)])
	}
	var numbers []int
	for line := range strings.SplitSeq(skill, "\n") {
		if rest, ok := strings.CutPrefix(line, "## "); ok {
			n, dotted := strings.CutSuffix(strings.Fields(rest)[0], ".")
			i, err := strconv.Atoi(n)
			if err != nil || !dotted {
				t.Errorf("heading %q is not numbered", line)
			}
			numbers = append(numbers, i)
		}
	}
	if !slices.Equal(numbers, []int{1, 2, 3, 4, 5, 6, 7, 8}) {
		t.Errorf("headings %v", numbers)
	}
}

// skillSpans checks each code span of the skill, a command that parses whole or a record kind,
// and is the verbs and the kinds they name.
func skillSpans(t *testing.T, skill string) (map[string]bool, map[string]bool) {
	t.Helper()
	verbs, kinds := map[string]bool{}, map[string]bool{}
	for _, m := range backtickRE.FindAllStringSubmatch(skill, -1) {
		span := m[1]
		rest, ok := strings.CutPrefix(span, "teamchat ")
		if !ok {
			if !slices.Contains(records.Kinds, span) {
				t.Errorf("SKILL.md: the code span %q is no command and no record kind", span)
			}
			kinds[span] = true
			continue
		}
		for _, cmd := range expand(fill(t, span, rest)) {
			args := strings.Fields(cmd)
			for i, a := range args {
				args[i] = unquote(a)
			}
			parses(t, "SKILL.md", args)
			verbs[args[0]] = true
		}
	}
	return verbs, kinds
}

// PK2, B 2.12: every verb, flag, record kind and exit class the skill names exists, and none an
// agent can meet is missing. A command is written in a code span and parses whole; every other
// code span is a record kind; prose names no command outside a span.
func TestTheSkillNamesWhatTheBinaryHas(t *testing.T) {
	skill := shippedSkill(t)
	agent := agentVerbs()
	verbs, kinds := skillSpans(t, skill)
	if found := scanned(t, "SKILL.md prose", backtickRE.ReplaceAllString(skill, "")); len(found) != 0 {
		t.Errorf("SKILL.md names commands outside code spans: %v", found)
	}
	for name := range agent {
		if !verbs[name] {
			t.Errorf("SKILL.md misses the verb %s", name)
		}
	}
	for _, k := range records.Kinds {
		if !kinds[k] {
			t.Errorf("SKILL.md misses the record kind %s", k)
		}
	}
	flags := map[string]bool{}
	for _, v := range agent {
		for _, f := range v.Flags {
			flags["--"+f.Name] = true
		}
	}
	for _, f := range flagRE.FindAllString(skill, -1) {
		if !flags[f] {
			t.Errorf("SKILL.md names the flag %s, which no agent verb has", f)
		}
	}
	if !strings.Contains(skill, "`teamchat help exit-codes`") {
		t.Error("SKILL.md does not point at teamchat help exit-codes")
	}
}

// classRE is one class line of the skill: the class, its exits, and what an agent does.
var classRE = regexp.MustCompile(`(?m)^- \*\*([a-z ]+)\*\* \(exits? ([0-9, ]+)\): (.+)\.$`)

// PK2, B 2.2.4: the skill teaches the four classes, each with its exits and its action as the
// exit table has them.
func TestTheSkillTeachesTheFourClasses(t *testing.T) {
	skill := shippedSkill(t)
	byClass := map[exits.Class][]string{}
	for _, row := range exits.Table() {
		byClass[row.Class] = append(byClass[row.Class], strconv.Itoa(int(row.Exit)))
	}
	got := make([]exits.Class, 0, len(exits.Classes))
	for _, m := range classRE.FindAllStringSubmatch(skill, -1) {
		c := exits.Class(m[1])
		got = append(got, c)
		if want := strings.Join(byClass[c], ", "); m[2] != want {
			t.Errorf("%s: exits %s, want %s", c, m[2], want)
		}
		if m[3] != c.Does() {
			t.Errorf("%s: %q, want %q", c, m[3], c.Does())
		}
	}
	if !slices.Equal(got, exits.Classes) {
		t.Errorf("classes %v, want %v", got, exits.Classes)
	}
}
