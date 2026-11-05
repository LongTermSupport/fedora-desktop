package proto

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"unicode/utf8"
)

// MaxQuestionsStateBytes is the most the store's `questions` key may hold (SPEC-n3f QX6's worst
// case): 192 questions, 1024 posts, 64 unattached posts, the longest token and a build record.
const MaxQuestionsStateBytes = 1_101_056

// The codes of a refused questions state: the strict decoder's own refusal of the document
// (`json`), and every other rule (`schema`).
const (
	CodeJSON   = "json"
	CodeSchema = "schema"
)

const maxStateToken = 1024

var (
	// ErrQuestionsState is a questions state, or an index state, that fails its codec.
	ErrQuestionsState = errors.New("proto: invalid questions state")

	hashRe = regexp.MustCompile(`^[0-9a-f]{64}$`)
	// tokenRe is the printable ASCII that encoding/json writes unescaped: no space, `"`, `\`,
	// `<`, `>` or `&`, so a token of 1024 bytes is 1024 bytes in the file (QX6).
	tokenRe = regexp.MustCompile(`^[\x21\x23-\x25\x27-\x3b\x3d\x3f-\x5b\x5d-\x7e]+$`)
)

// QuestionsStateError is a questions state refused by its codec: the code and the rule it broke.
// It never quotes a value from the document.
type QuestionsStateError struct{ Code, Rule string }

func (e *QuestionsStateError) Error() string {
	return "proto: the questions state is invalid (" + e.Code + "): " + e.Rule
}

// Is makes errors.Is(err, ErrQuestionsState) match.
func (e *QuestionsStateError) Is(target error) bool { return target == ErrQuestionsState }

// BuildRecord is a rebuild of the index owed or under way (SPEC-n3f QX5, n3f-D21). Start is the
// newest event of the build's first backward page, null while the build is owed; After the
// event the replaced index had folded the items of, so the build takes items only past it; and
// LateFrom the replaced index's Next(). A null After or LateFrom is a build begun with no index.
type BuildRecord struct {
	Start    *string `json:"start"`
	After    *string `json:"after"`
	LateFrom *int64  `json:"late_from"`
}

// QuestionsState is the store's `questions` key (QX5): the index's state, the `/messages` token
// after its last folded event and that event's id, and the build record. An index at the head
// holds the first three and no build record; a build owed holds the build record alone; a build
// under way holds all four. The store's other keys, and the coordinator's provisional entries,
// are never in it.
type QuestionsState struct {
	Index *IndexState  `json:"index"`
	Token *string      `json:"token"`
	Last  *string      `json:"last"`
	Build *BuildRecord `json:"build"`
}

// Encode is the key's compact JSON, refused unless DecodeQuestionsState reads it back, so
// nothing is written that would not be read.
func (s QuestionsState) Encode(server string) ([]byte, error) {
	data, err := json.Marshal(s)
	if err != nil {
		return nil, &QuestionsStateError{Code: CodeSchema, Rule: "a value that is not set or not of its grammar"}
	}
	if _, err := DecodeQuestionsState(data, server); err != nil {
		return nil, err
	}
	return data, nil
}

// DecodeQuestionsState decodes the key as member/state's files are: at most its cap, proto's
// strict JSON (valid UTF-8, no repeated key, integers only within 2^53, nothing after the
// value), an object decoded into its type with no key it does not have, which must encode back
// to the same JSON value (so a missing key, a key in another case or a value of another form is
// refused), and then Check.
func DecodeQuestionsState(data []byte, server string) (QuestionsState, error) {
	fail := func(code, rule string) (QuestionsState, error) {
		return QuestionsState{}, &QuestionsStateError{Code: code, Rule: rule}
	}
	if len(data) > MaxQuestionsStateBytes {
		return fail(CodeSchema, fmt.Sprintf("larger than its cap of %d bytes", MaxQuestionsStateBytes))
	}
	v, err := Parse(data)
	if syntax, ok := errors.AsType[*SyntaxError](err); ok {
		return fail(CodeJSON, syntax.Error())
	}
	if v.Kind() != KindObject {
		return fail(CodeSchema, "not an object")
	}
	var s QuestionsState
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if dec.Decode(&s) != nil {
		return fail(CodeSchema, "a key it does not have, or a value not of its key's type")
	}
	if !sameJSON(s, v) {
		return fail(CodeSchema, "a key missing or in another case")
	}
	if err := s.Check(server); err != nil {
		return QuestionsState{}, err
	}
	return s, nil
}

// sameJSON is whether v encodes to the JSON value want.
func sameJSON(v any, want Value) bool {
	again, err := json.Marshal(v)
	if err != nil {
		return false
	}
	back, err := Parse(again)
	return err == nil && back.Equal(want)
}

// stateRules keeps the first rule a state broke.
type stateRules struct{ rule string }

func (c *stateRules) need(ok bool, rule string) {
	if !ok && c.rule == "" {
		c.rule = rule
	}
}

func (c *stateRules) err() error {
	if c.rule == "" {
		return nil
	}
	return &QuestionsStateError{Code: CodeSchema, Rule: c.rule}
}

// Check is the rules of the key (QX5, QX6): the combinations of index, token, last event and
// build record that are valid, and the index and the token themselves.
func (s QuestionsState) Check(server string) error {
	c := &stateRules{}
	s.checkBuild(c)
	if s.Index != nil {
		s.Index.check(c, server)
	}
	if s.Token != nil {
		c.need(*s.Token != "", "token is empty")
		c.need(len(*s.Token) <= maxStateToken, "token is longer than 1024 bytes")
		c.need(*s.Token == "" || tokenRe.MatchString(*s.Token), "token has a character that is not printable ASCII or needs a JSON escape")
	}
	if s.Last != nil {
		c.need(ValidEventID(*s.Last), "last is not an event id")
	}
	return c.err()
}

// checkBuild is which combinations of index, token, last event and build record are valid: no
// build record with the three; a build owed (start null) with none of them and the event it
// had taken the items to; a build under way (start set) with the three.
func (s QuestionsState) checkBuild(c *stateRules) {
	b := s.Build
	whole := s.Index != nil && s.Token != nil && s.Last != nil
	none := s.Index == nil && s.Token == nil && s.Last == nil
	c.need(b != nil || whole, "an index at the head needs its state, token and last event")
	if b == nil {
		return
	}
	c.need(b.Start != nil || none, "a build owed holds no state, token or last event")
	c.need(b.Start != nil || b.After != nil, "a build owed names the event its items were taken to")
	c.need(b.Start == nil || whole, "a build under way holds its state, token and last event")
	c.need(b.Start == nil || ValidEventID(*b.Start), "start is not an event id")
	c.need(b.After == nil || ValidEventID(*b.After), "after is not an event id")
	c.need(b.LateFrom == nil || *b.LateFrom >= 1 && *b.LateFrom <= maxSafeInt, "late_from is not a question number")
}

// Check is the rules of the index's stored state (QX5, QX6, G1.7).
func (s IndexState) Check(server string) error {
	c := &stateRules{}
	s.check(c, server)
	return c.err()
}

func (s IndexState) check(c *stateRules, server string) {
	c.need(s.Highest >= 0 && s.Highest <= maxSafeInt, "highest is negative or past 2^53")
	c.need(s.Slots >= 0 && s.Slots <= maxSafeInt, "slots is negative or past 2^53")
	c.need(s.Evicted >= 0 && s.Evicted <= maxSafeInt, "evicted is negative or past 2^53")
	c.need(s.Highest+s.Slots+s.Evicted+1 <= maxSafeInt, "highest, slots and evicted add up past 2^53")
	s.checkQuestions(c, server)
	s.checkPosts(c)
	s.checkUnattached(c, server)
}

func (s IndexState) checkQuestions(c *stateRules, server string) {
	c.need(s.Questions != nil, "questions is null")
	open, closed, last := 0, 0, int64(0)
	for _, q := range s.Questions {
		c.need(q.N > last, "question numbers are not ascending and distinct from 1")
		c.need(q.N <= s.Highest, "a question's number is past the highest")
		last = q.N
		if q.Closed {
			closed++
		} else {
			open++
		}
		q.check(c, server)
	}
	c.need(open <= MaxOpenQuestions, "more than 64 questions are not closed")
	c.need(closed <= MaxClosedQuestions, "more than 128 questions are closed")
}

func (s IndexState) checkPosts(c *stateRules) {
	c.need(s.Posts != nil, "posts is null")
	c.need(len(s.Posts) <= MaxAttachments, "more than 1024 posts")
	seen := make(map[string]bool, len(s.Posts))
	for _, p := range s.Posts {
		c.need(ValidEventID(p.ID), "a post's id is not an event id")
		c.need(!seen[p.ID], "a post id is repeated")
		seen[p.ID] = true
		c.need(p.T >= postHuman && p.T <= postSlot, "a post's kind is not 0 to 3")
		c.need(p.Q >= 0, "a post's question is negative")
		c.need(p.Q > 0 || p.T != postHumanThread && p.T != postAgent, "a thread post has no question")
		c.need(p.Q == 0 || p.T != postSlot, "a slot has a question")
	}
}

func (s IndexState) checkUnattached(c *stateRules, server string) {
	c.need(s.Unattached != nil, "unattached is null")
	c.need(len(s.Unattached) <= MaxUnattached, "more than 64 unattached posts")
	seen := make(map[string]bool, len(s.Unattached))
	for _, u := range s.Unattached {
		c.need(ValidEventID(u.ID), "an unattached post's id is not an event id")
		c.need(!seen[u.ID], "an unattached id is repeated")
		seen[u.ID] = true
		c.need(validIndexUserOf(u.From, server), "an unattached post's from_id is not a user id of the server")
		c.need(u.SentMs >= 0, "an unattached post's sent_ms is negative")
	}
}

// check is the rules of one stored question: an entry that no event holds is a number held by a
// redacted event (QX7), and the rest is a counted question.
func (q IndexedQuestion) check(c *stateRules, server string) {
	c.need(q.AskedBy != nil && q.Re != nil && q.Unanswered != nil, "a list of a question is null")
	if q.EventID == "" {
		q.checkHeld(c)
		return
	}
	c.need(ValidEventID(q.EventID), "a question's id is not an event id")
	c.need(ValidEventID(q.Latest), "a question's latest is not an event id")
	c.need(hashRe.MatchString(q.Hash), "a question's hash is not a SHA-256 in lower-case hex")
	c.need(headlineRunes(q.Headline) >= 1 && headlineRunes(q.Headline) <= MaxHeadline, "a question's headline is not 1 to 120 code points")
	c.need(distinctList(q.AskedBy, 0, MaxAskedBy, func(id string) bool { return validIndexUserOf(id, server) }), "asked_by is not at most 16 distinct user ids of the server")
	c.need(distinctList(q.Re, 0, MaxQuestionRe, ValidEventID), "re is not at most 16 distinct event ids")
	c.need(q.AskedMs >= 0, "asked_ms is negative")
	c.need(q.RemindAfterS == 0 || q.RemindAfterS >= MinRemindS && q.RemindAfterS <= MaxRemindS, "remind_after_s is not 0 or 600 to 604800")
	c.need(q.Attached >= 0, "attached is negative")
	c.need(distinctList(q.Unanswered, 0, MaxUnanswered, ValidEventID), "unanswered is not at most 16 distinct event ids")
	c.need(q.UnansweredMore >= 0, "unanswered_more is negative")
	c.need(q.UnansweredMore == 0 || len(q.Unanswered) == MaxUnanswered, "unanswered_more is set with fewer than 16 unanswered")
	c.need(q.Attached >= int64(len(q.Unanswered))+q.UnansweredMore, "attached is fewer than its unanswered posts")
}

func (q IndexedQuestion) checkHeld(c *stateRules) {
	c.need(q.Closed, "a held number is not closed")
	c.need(q.Latest == "" && q.Headline == "" && q.Hash == "", "a held number has text")
	c.need(len(q.AskedBy) == 0 && len(q.Re) == 0 && len(q.Unanswered) == 0, "a held number has a list")
	c.need(q.AskedMs == 0 && q.RemindAfterS == 0 && q.Attached == 0 && q.UnansweredMore == 0, "a held number has a count")
}

func headlineRunes(s string) int { return utf8.RuneCountInString(s) }

// validIndexUserOf is a user ID the index keeps (the fold's validIndexUser) of this server.
func validIndexUserOf(id, server string) bool {
	return len(id) <= maxIndexedUserID && ClassifyUserID(id, server) != IDNone
}

// LoadQuestionIndex is the index of a stored state, the state refused unless it passes Check. The
// counting starts from the highest of the state and the roster's `question_floor`; the lookups
// the fold makes (the roots of the questions with an event, the posts, the unattached posts) are
// rebuilt from it. The index shares nothing with s.
func LoadQuestionIndex(server string, floor int64, s IndexState) (*QuestionIndex, error) {
	if err := s.Check(server); err != nil {
		return nil, err
	}
	x := NewQuestionIndex(server, max(floor, s.Highest))
	x.slots, x.evicted = s.Slots, s.Evicted
	for _, q := range s.Questions {
		kept := q.clone()
		x.questions[q.N] = &kept
		if q.EventID != "" {
			x.roots[q.EventID] = q.N
		}
	}
	for _, p := range s.Posts {
		x.posts.add(p)
	}
	x.unattached = slices.Clone(s.Unattached)
	return x, nil
}
