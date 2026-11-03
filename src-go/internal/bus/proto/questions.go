package proto

import (
	"errors"
	"strconv"
	"strings"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/pingbus/pyjson"
)

// The question sections (Plan 00211 SPEC §8.2), in the order a line is matched against them.
const (
	sectionContext  = "Context:"
	sectionQuestion = "Question:"
	sectionOptions  = "Options:"
	optionsMax      = 9
	roomMessageType = "m.room.message"
)

var questionSections = []string{sectionContext, sectionQuestion, sectionOptions}

// ErrThreadTarget is a thread post with neither its question's root nor the event it answers.
var ErrThreadTarget = errors.New("a thread post names its question or the event it answers")

// sections is the Context:, Question: and Options: sections from line 2 on, each from its line
// to the next section's, stripped; a section named twice is refused body.
func sections(text string) (map[string]string, error) {
	found := map[string][]string{}
	current := ""
	for _, line := range strings.Split(text, "\n")[1:] {
		name := sectionOf(line)
		switch {
		case name != "":
			if _, twice := found[name]; twice {
				return nil, refuseBody("the " + name + " section is given twice")
			}
			found[name] = []string{line[len(name):]}
			current = name
		case current != "":
			found[current] = append(found[current], line)
		}
	}
	out := make(map[string]string, len(found))
	for name, lines := range found {
		out[name] = pyStrip(strings.Join(lines, "\n"))
	}
	return out, nil
}

func sectionOf(line string) string {
	for _, name := range questionSections {
		if strings.HasPrefix(line, name) {
			return name
		}
	}
	return ""
}

// optionNumber is the digit of a line that starts `<1-9>.` and a Python whitespace character,
// a line end counting as one, else 0.
func optionNumber(line string) int {
	line += " "
	if line[0] < '1' || line[0] > '9' || line[1] != '.' || !pySpace([]rune(line[2:])[0]) {
		return 0
	}
	return int(line[0] - '0')
}

func checkOptions(options string) error {
	lines := []string{}
	for line := range strings.SplitSeq(options, "\n") {
		if pyStrip(line) != "" {
			lines = append(lines, line)
		}
	}
	bad := len(lines) == 0 || len(lines) > optionsMax
	for i, line := range lines {
		bad = bad || optionNumber(line) != i+1
	}
	if bad {
		return refuseBody("Options: are lines numbered 1. to " + strconv.Itoa(optionsMax) + "., in order")
	}
	return nil
}

// CheckQuestionText is SPEC P54: a headline, Context: and Question: sections, an optional
// Options: numbered 1. to 9., at most 8000 bytes, no secret's shape. It returns the headline
// and the stripped Question: section.
func CheckQuestionText(text string) (string, string, error) {
	if !withinBytes(text, maxQuestionBytes) {
		return "", "", refuseBody("a question is at most " + strconv.Itoa(maxQuestionBytes) + " bytes")
	}
	headline, err := checkHeadline(text)
	if err != nil {
		return "", "", err
	}
	if sectionOf(headline) != "" {
		return "", "", refuseBody("the first line is the headline, before the Context: section")
	}
	found, err := sections(text)
	if err != nil {
		return "", "", err
	}
	for _, name := range []string{sectionContext, sectionQuestion} {
		if found[name] == "" {
			return "", "", refuseBody("the " + name + " section is missing")
		}
	}
	if options, given := found[sectionOptions]; given {
		if err := checkOptions(options); err != nil {
			return "", "", err
		}
	}
	if SecretShape(text) != "" {
		return "", "", refuse(RefuseSecret)
	}
	return headline, found[sectionQuestion], nil
}

func anyStrings(items []string) []any {
	out := make([]any, len(items))
	for i, s := range items {
		out[i] = s
	}
	return out
}

func questionPrefix(n int64) string { return "Q" + strconv.FormatInt(n, 10) + " " }

// BuildQuestion is SPEC P55: a question as a thread root, m.text mentioning every human so
// Element notifies, its text numbered Q<n>, and whom to answer.
func BuildQuestion(n int64, text string, humans, askedBy, re []string) pyjson.Object {
	return pyjson.Object{
		msgtypeKey: textMsgtype, bodyKey: questionPrefix(n) + text,
		mentionsKey: pyjson.Object{"user_ids": anyStrings(sortedCopy(humans))},
		KeyQuestion: pyjson.Object{
			"v": intNum(1), "n": intNum(n), "asked_by": anyStrings(askedBy), "re": anyStrings(re),
		},
	}
}

// Question is a parsed question (SPEC P56).
type Question struct {
	N        int64
	EventID  string
	Sender   string
	Headline string
	Question string
	AskedBy  []string
	Re       []string
}

// messageParts is the event ID, sender and content of an m.room.message event with an event
// ID, a string sender and an object content.
func messageParts(event any) (string, string, pyjson.Object, bool) {
	obj, ok := event.(pyjson.Object)
	if !ok || obj["type"] != roomMessageType {
		return "", "", nil, false
	}
	id, isID := obj[eventIDKey].(string)
	sender, isSender := obj["sender"].(string)
	content, isObj := obj["content"].(pyjson.Object)
	if !isID || !IsEventID(id) || !isSender || !isObj {
		return "", "", nil, false
	}
	return id, sender, content, true
}

func anyString(string) bool { return true }

// ParseQuestion is SPEC P56: the question an agent's ask-human post is, or false. A number
// past int64 is not one (D11); a body with a lone surrogate is not one (D10).
func ParseQuestion(event any, serverName string) (*Question, bool) {
	id, sender, content, ok := messageParts(event)
	if !ok || !IsAgentUserID(sender, serverName) || content[msgtypeKey] != textMsgtype {
		return nil, false
	}
	q, valid := questionFields(content)
	if !valid {
		return nil, false
	}
	body, isStr := content[bodyKey].(string)
	if !isStr || !strings.HasPrefix(body, questionPrefix(q.N)) {
		return nil, false
	}
	headline, question, err := CheckQuestionText(body[len(questionPrefix(q.N)):])
	if err != nil {
		return nil, false
	}
	q.EventID, q.Sender, q.Headline, q.Question = id, sender, headline, question
	return q, true
}

// questionFields is the number, askers and events answered of a valid agent_bus.question object.
func questionFields(content pyjson.Object) (*Question, bool) {
	obj, isObj := content[KeyQuestion].(pyjson.Object)
	if !isObj || !hasExactKeys(obj, "v", "n", "asked_by", "re") || !pyjson.EqualsInt(obj["v"], 1) {
		return nil, false
	}
	num, isNum := obj["n"].(pyjson.Number)
	n, fits := num.Int64()
	askedBy, isList := obj["asked_by"].([]any)
	askers, allStr := stringsWhere(askedBy, anyString)
	reList, isReList := obj["re"].([]any)
	re, allIDs := stringsWhere(reList, IsEventID)
	if !isNum || !fits || n < 1 || !isList || !allStr || !isReList || !allIDs {
		return nil, false
	}
	return &Question{N: n, AskedBy: askers, Re: re}, true
}

// ThreadPost is what a coordinator posts in a question's thread (SPEC P57): Root is the
// question's event ID, or "" for a plain reply to Re; Re is the event it answers, or "";
// Ask mentions the humans; Close is the number of the question it closes, or 0.
type ThreadPost struct {
	Root   string
	Re     string
	Text   string
	Humans []string
	Ask    bool
	Close  int64
}

// BuildThreadPost is SPEC P57; neither Root nor Re is ErrThreadTarget.
func BuildThreadPost(post ThreadPost) (pyjson.Object, error) {
	if post.Root == "" && post.Re == "" {
		return nil, ErrThreadTarget
	}
	body := post.Text
	if body == "" && post.Close != 0 {
		body = "Q" + strconv.FormatInt(post.Close, 10) + " closed."
	}
	content := pyjson.Object{msgtypeKey: noticeMsgtype, bodyKey: body, mentionsKey: pyjson.Object{}}
	if post.Ask {
		content[msgtypeKey] = textMsgtype
		content[mentionsKey] = pyjson.Object{"user_ids": anyStrings(sortedCopy(post.Humans))}
	}
	content["m.relates_to"] = threadRelatesTo(post.Root, post.Re)
	if post.Close != 0 {
		content[KeyQuestionState] = pyjson.Object{"v": intNum(1), "n": intNum(post.Close), "state": StateClosed}
	}
	return content, nil
}

func threadRelatesTo(root, re string) pyjson.Object {
	if root == "" {
		return pyjson.Object{"m.in_reply_to": pyjson.Object{eventIDKey: re}}
	}
	target := re
	if target == "" {
		target = root
	}
	return pyjson.Object{
		"rel_type": threadRelation, eventIDKey: root, "is_falling_back": re == "",
		"m.in_reply_to": pyjson.Object{eventIDKey: target},
	}
}
