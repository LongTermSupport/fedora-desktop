package hooks

// Future home: bus/proto (protocol.validate_event and everything it reaches): the offline
// receive steps that decide whether a stored inbox item still counts (H26, H28).

import (
	"errors"
	"math/big"
	"regexp"
	"slices"
	"strings"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/pingbus/pyjson"
)

const (
	outcomeAccept = "accept"
	outcomeIgnore = "ignore"
	outcomeDrop   = "drop"

	classHuman = "human"
	classPeer  = "peer"
	classPing  = "ping"

	roomMain         = "main"
	purposeReports   = "reports"
	purposeQuestions = "questions"

	eventMessage = "m.room.message"
	msgtypeHuman = "m.text"
	msgtypeAgent = "m.notice"
	pingKey      = "agent_bus.ping"
	textKey      = "agent_bus.text"
	relatesKey   = "m.relates_to"
	newContent   = "m.new_content"
	mentionsKey  = "m.mentions"
	inReplyTo    = "m.in_reply_to"
	relReplace   = "m.replace"
	senderHuman  = "human"
	renderTag    = "[agent-bus]"

	maxContentBytes   = 4096
	maxHumanBodyBytes = 16384
	maxAgentTextBytes = 4096
	maxRefLen         = 700
	maxTo             = 32
	maxTextTo         = 16

	ruleRequired  = "required"
	ruleOptional  = "optional"
	ruleForbidden = "forbidden"
	senderAddress = "addressed"
	refFormPath   = "path"
	refFormCommit = "commit"
)

var (
	eventIDPattern = regexp.MustCompile(`^\$[A-Za-z0-9_-]{43}$`)
	ownerRepo      = `([a-z0-9](?:[a-z0-9-]{0,37}[a-z0-9])?)/([a-z0-9._-]{1,100})`
	refSHA         = `[0-9a-f]{40}`
	refPath        = `[A-Za-z0-9._-]{1,64}(?:/[A-Za-z0-9._-]{1,64}){0,7}`
	pathPattern    = regexp.MustCompile(`^` + refPath + `$`)
	// The forms in protocol._REF_RES order; the path's PATH is the last group.
	refPatterns = []struct {
		form string
		re   *regexp.Regexp
	}{
		{refFormPath, regexp.MustCompile(`^path:` + ownerRepo + `@` + refSHA + `:(` + refPath + `)$`)},
		{refFormCommit, regexp.MustCompile(`^commit:` + ownerRepo + `@` + refSHA + `$`)},
		{"pr", regexp.MustCompile(`^pr:` + ownerRepo + `#[1-9][0-9]{0,9}@` + refSHA + `$`)},
		{"issue", regexp.MustCompile(`^issue:` + ownerRepo + `#[1-9][0-9]{0,9}$`)},
	}
	contentKeys     = []string{"msgtype", "body", mentionsKey, pingKey}
	pingKeys        = []string{"v", "verb", "to", "ref", "re", "branch"}
	pingRequired    = []string{"v", "verb", "to"}
	textContentKeys = []string{"msgtype", "body", mentionsKey, textKey}
	textObjectKeys  = []string{"v", "to", "text"}
	branchRefForms  = []string{refFormPath, refFormCommit}
	allRefForms     = []string{refFormPath, refFormCommit, "pr", "issue"}
	coordinatorOnly = []string{roleCoordinator}
	anyRole         = []string{roleCoordinator, roleWorker}
	verbsToHumans   = []string{"ack", "nack", "done", "blocked"}
	verbs           = map[string]verbRule{
		"fetch":   {ruleRequired, branchRefForms, ruleForbidden, coordinatorOnly, true},
		"sync":    {ruleRequired, branchRefForms, ruleForbidden, coordinatorOnly, false},
		"review":  {ruleRequired, []string{refFormPath, refFormCommit, "pr"}, ruleForbidden, anyRole, true},
		"run-qa":  {ruleRequired, []string{refFormCommit, "pr"}, ruleForbidden, anyRole, false},
		"halt":    {ruleForbidden, nil, ruleForbidden, coordinatorOnly, false},
		"ack":     {ruleForbidden, nil, ruleRequired, []string{senderAddress}, false},
		"nack":    {ruleOptional, allRefForms, ruleRequired, []string{senderAddress}, true},
		"done":    {ruleRequired, allRefForms, ruleOptional, anyRole, true},
		"blocked": {ruleRequired, allRefForms, ruleOptional, anyRole, true},
	}
)

// verbRule is protocol.VerbRule without the acknowledgement, which receiving does not use.
type verbRule struct {
	ref      string
	refForms []string
	re       string
	senders  []string
	external bool
}

// outcome is protocol.Outcome as the hooks count it: the kind, the reason, which accepted item
// it is and its origin_server_ts.
type outcome struct {
	kind, reason, class string
	ts                  *big.Int
}

// refusal is protocol.Refusal: a drop with its reason.
type refusal struct{ reason string }

func (r *refusal) Error() string { return r.reason }

func drop(reason string) outcome   { return outcome{kind: outcomeDrop, reason: reason} }
func ignore(reason string) outcome { return outcome{kind: outcomeIgnore, reason: reason} }
func refuse(reason string) error   { return &refusal{reason} }

// isEventID is protocol.is_event_id.
func isEventID(value any) bool { return matches(eventIDPattern, value) }

// utf8Len is len(text.encode("utf-8")); a lone surrogate cannot be encoded, which v1 raises
// (UnicodeEncodeError, uncaught: internal).
func utf8Len(text string) (int, error) {
	if hasSurrogate(text) {
		return 0, internalErr("UnicodeEncodeError")
	}
	return len(text), nil
}

// validateEvent is protocol.validate_event for one stored item of room, with no seen set. An
// error is an exception v1 raises (a TypeError or UnicodeEncodeError: the hook's internal).
func validateEvent(value any, record teamRecord, server, self string, humanText bool, room string) (outcome, error) {
	event, ok := value.(pyjson.Object)
	if !ok {
		return drop("schema"), nil
	}
	if kind, isText := event["type"].(string); !isText || kind != eventMessage {
		return ignore("type"), nil
	}
	if !isEventID(event["event_id"]) {
		return drop("schema"), nil
	}
	sender := event["sender"]
	if name, isText := sender.(string); isText && name == self {
		return ignore("self"), nil
	}
	unsigned, isObject := event["unsigned"].(pyjson.Object)
	_, redacted := unsigned["redacted_because"]
	_, stateKey := event["state_key"]
	content, isContent := event["content"].(pyjson.Object)
	ts, isTS := originTS(event["origin_server_ts"])
	if stateKey || (isObject && redacted) || !isContent || !isTS {
		return drop("schema"), nil
	}
	v := receiving{record: record, server: server, self: self, humanText: humanText, content: content, sender: sender}
	found, err := v.outcome(room)
	if err != nil {
		return outcome{}, err
	}
	if found.kind == outcomeAccept {
		found.ts = ts
	}
	return found, nil
}

// originTS is `type(ts) is int and ts >= 0`.
func originTS(value any) (*big.Int, bool) {
	n, ok := value.(pyjson.Number)
	if !ok || n.Kind != pyjson.Int {
		return nil, false
	}
	ts, ok := new(big.Int).SetString(n.Lit, 10)
	return ts, ok && ts.Sign() >= 0
}

// receiving is one event's validation: the context and the parts of the event already read.
type receiving struct {
	record    teamRecord
	server    string
	self      string
	humanText bool
	content   pyjson.Object
	sender    any
}

func (v *receiving) outcome(room string) (outcome, error) {
	class := v.senderClass(v.sender)
	if room == purposeQuestions {
		return v.questionsOutcome(class)
	}
	switch class {
	case "":
		return drop("sender"), nil
	case senderHuman:
		return v.humanOutcome()
	}
	if _, isText := v.content[textKey]; isText {
		if _, isPing := v.content[pingKey]; isPing {
			return drop("schema"), nil
		}
		return v.agentTextOutcome()
	}
	return v.pingOutcome()
}

// senderClass is protocol.sender_class: human for a listed human, the role an agent holds,
// else "" (drop sender).
func (v *receiving) senderClass(value any) string {
	user, ok := value.(string)
	switch {
	case !ok:
		return ""
	case v.record.humans[user]:
		return senderHuman
	}
	return v.record.roles[user]
}

// roleOf is ctx.roles.get(value): a list or an object cannot be hashed, which v1 raises.
func (v *receiving) roleOf(value any) (string, error) {
	switch user := value.(type) {
	case []any, pyjson.Object:
		return "", internalErr("TypeError")
	case string:
		return v.record.roles[user], nil
	}
	return "", nil
}

func (v *receiving) pingOutcome() (outcome, error) {
	verb, to, err := v.validateContent()
	if err == nil {
		err = v.checkRole(verb)
	}
	if r, ok := errors.AsType[*refusal](err); ok {
		return drop(r.reason), nil
	}
	if err != nil {
		return outcome{}, err
	}
	if !slices.Contains(to, v.self) {
		return ignore("not-addressed"), nil
	}
	return outcome{kind: outcomeAccept, class: classPing}, nil
}

// checkRole is protocol.check_role.
func (v *receiving) checkRole(verb string) error {
	rule := verbs[verb]
	class := v.senderClass(v.sender)
	if class != roleCoordinator && class != roleWorker {
		return refuse("role")
	}
	if !slices.Contains(rule.senders, senderAddress) && !slices.Contains(rule.senders, class) {
		return refuse("role")
	}
	return nil
}

// versionFirst is protocol._check_version_first: an integer v other than 1 is version.
func versionFirst(value any) error {
	obj, ok := value.(pyjson.Object)
	if !ok {
		return nil
	}
	if n, isNumber := obj["v"].(pyjson.Number); isNumber && n.Kind == pyjson.Int && !n.IsInt(1) {
		return refuse("version")
	}
	return nil
}

// validateContent is protocol.validate_content: the ping's verb and its sorted targets.
func (v *receiving) validateContent() (string, []string, error) {
	content := v.content
	if err := versionFirst(content[pingKey]); err != nil {
		return "", nil, err
	}
	if hasAny(content, relatesKey, newContent) {
		return "", nil, refuse("edit")
	}
	dumped, err := pyDump(content, styleUnicode)
	switch {
	case err != nil:
		return "", nil, refuse("schema")
	case len(dumped) > maxContentBytes:
		return "", nil, refuse("size")
	}
	obj, err := pingObject(content)
	if err != nil {
		return "", nil, err
	}
	verb := text(obj["verb"])
	rule, known := verbs[verb]
	if !known {
		return "", nil, refuse("verb")
	}
	to, err := v.checkTargets(texts(listValue(obj["to"])), verb, maxTo)
	if err != nil {
		return "", nil, err
	}
	if err := v.checkPingRefs(obj, rule); err != nil {
		return "", nil, err
	}
	if !mentionsAre(content[mentionsKey], to) {
		return "", nil, refuse("schema")
	}
	body, isText := content["body"].(string)
	if !isText {
		return "", nil, refuse("schema")
	}
	if body != renderPing(obj, to) {
		return "", nil, refuse("body")
	}
	return verb, to, nil
}

// pingObject is the ping's keys and types checked: the agent_bus.ping object.
func pingObject(content pyjson.Object) (pyjson.Object, error) {
	if !keysAre(content, contentKeys) || !isString(content["msgtype"], msgtypeAgent) {
		return nil, refuse("schema")
	}
	obj, ok := content[pingKey].(pyjson.Object)
	if !ok {
		return nil, refuse("schema")
	}
	for key := range obj {
		if !slices.Contains(pingKeys, key) {
			return nil, refuse("schema")
		}
	}
	for _, key := range pingRequired {
		if _, present := obj[key]; !present {
			return nil, refuse("schema")
		}
	}
	for key, value := range obj {
		if !pingTypeOK(key, value) {
			return nil, refuse("schema")
		}
	}
	return obj, nil
}

// pingTypeOK is protocol._check_ping_types for one key.
func pingTypeOK(key string, value any) bool {
	switch key {
	case "v":
		n, ok := value.(pyjson.Number)
		return ok && n.Kind == pyjson.Int
	case "to":
		list, ok := value.([]any)
		return ok && all(list, func(x any) bool { _, s := x.(string); return s })
	}
	_, ok := value.(string)
	return ok
}

// checkPingRefs is the ref, branch and re of a ping (_check_ref, check_branch, check_re).
func (v *receiving) checkPingRefs(obj pyjson.Object, rule verbRule) error {
	ref, err := v.checkRef(obj["ref"], rule)
	switch {
	case errors.Is(err, errNoRef):
		ref = nil
	case err != nil:
		return err
	}
	if branch, present := obj["branch"]; present {
		if ref == nil || !slices.Contains(branchRefForms, ref.form) || !matches(branchPattern, branch) {
			return refuse("ref")
		}
	}
	re, present := obj["re"]
	switch {
	case !present && rule.re == ruleRequired:
		return refuse("re")
	case present && (rule.re == ruleForbidden || !isEventID(re)):
		return refuse("re")
	}
	return nil
}

// ref is protocol.Ref, the parts the receive checks use.
type ref struct{ form, owner, repo, path string }

// checkRef is protocol._check_ref: the reference's form, then its trust; errNoRef when there
// is none and the verb allows that.
func (v *receiving) checkRef(value any, rule verbRule) (*ref, error) {
	if value == nil {
		if rule.ref == ruleRequired {
			return nil, refuse("ref")
		}
		return nil, errNoRef
	}
	if rule.ref == ruleForbidden {
		return nil, refuse("ref")
	}
	found := parseRef(text(value))
	if found == nil || !slices.Contains(rule.refForms, found.form) {
		return nil, refuse("ref")
	}
	return found, v.trustRef(found, rule)
}

// errNoRef is a ping without a reference, which its verb allows.
var errNoRef = errors.New("no reference")

// trustRef is the trust half of protocol._check_ref (spec section 11).
func (v *receiving) trustRef(found *ref, rule verbRule) error {
	r := v.record
	inOrg := r.hasOrg && found.owner == r.org
	if inOrg && slices.Contains(r.exclude, found.repo) {
		return refuse("excluded")
	}
	if _, listed := r.repos[found.owner+"/"+found.repo]; !listed && !inOrg {
		if !rule.external {
			return refuse("allowlist")
		}
		return nil
	}
	if found.form == refFormPath && len(r.pathPrefixes) > 0 && !pathAllowed(found.path, r.pathPrefixes) {
		return refuse("allowlist")
	}
	return nil
}

// parseRef is protocol.parse_ref.
func parseRef(value string) *ref {
	if pyLen(value) > maxRefLen {
		return nil
	}
	for _, p := range refPatterns {
		m := p.re.FindStringSubmatch(value)
		if m == nil {
			continue
		}
		found := &ref{form: p.form, owner: m[1], repo: m[2]}
		if !isRepoNameText(found.repo) {
			return nil
		}
		if p.form == refFormPath {
			found.path = m[3]
			if !pathOK(found.path) {
				return nil
			}
		}
		return found
	}
	return nil
}

// pathOK is protocol._path_ok.
func pathOK(path string) bool {
	return pathPattern.MatchString(path) && !slices.ContainsFunc(strings.Split(path, "/"), func(s string) bool {
		return s == "." || s == ".."
	})
}

// pathAllowed is protocol.path_allowed.
func pathAllowed(path string, prefixes []string) bool {
	return slices.ContainsFunc(prefixes, func(prefix string) bool {
		if strings.HasSuffix(prefix, "/") {
			return strings.HasPrefix(path, prefix)
		}
		return path == prefix
	})
}

// renderPing is protocol.render.
func renderPing(obj pyjson.Object, to []string) string {
	ref, present := obj["ref"]
	shown := "-"
	if present {
		shown = text(ref)
	}
	out := renderTag + " " + text(obj["verb"]) + " " + shown
	if branch, present := obj["branch"]; present {
		out += " on " + text(branch)
	}
	out += " -> " + strings.Join(to, " ")
	if re, present := obj["re"]; present {
		out += " re " + text(re)
	}
	return out
}

// checkTargets is protocol._check_targets (most maxTo, a verb) and _check_text_targets (most
// maxTextTo, no verb): distinct, addressable, sorted.
func (v *receiving) checkTargets(to []string, verb string, most int) ([]string, error) {
	sorted := slices.Sorted(slices.Values(to))
	if len(to) < 1 || len(to) > most || len(slices.Compact(slices.Clone(sorted))) != len(to) {
		return nil, refuse("target")
	}
	for _, user := range to {
		if !v.addressable(user, verb) {
			return nil, refuse("target")
		}
	}
	return sorted, nil
}

// addressable is protocol.addressable; verb "" is an agent text.
func (v *receiving) addressable(user, verb string) bool {
	if role := v.record.roles[user]; role != "" {
		if _, _, agent := agentUser(user, v.server); agent {
			return true
		}
	}
	return (verb == "" || slices.Contains(verbsToHumans, verb)) && v.record.humans[user] && isHumanUser(user, v.server)
}

// mentionsAre is `value == {"user_ids": list(to)}`.
func mentionsAre(value any, to []string) bool {
	mentions, ok := value.(pyjson.Object)
	if !ok || len(mentions) != 1 {
		return false
	}
	users, ok := mentions["user_ids"].([]any)
	if !ok || len(users) != len(to) {
		return false
	}
	for i, user := range users {
		if name, isText := user.(string); !isText || name != to[i] {
			return false
		}
	}
	return true
}

func keysAre(object pyjson.Object, keys []string) bool {
	if len(object) != len(keys) {
		return false
	}
	for _, key := range keys {
		if _, present := object[key]; !present {
			return false
		}
	}
	return true
}

func hasAny(object pyjson.Object, keys ...string) bool {
	return slices.ContainsFunc(keys, func(key string) bool { _, present := object[key]; return present })
}

func isString(value any, want string) bool {
	got, ok := value.(string)
	return ok && got == want
}

// humanOutcome is protocol._human_outcome: a listed human's message in the main room.
func (v *receiving) humanOutcome() (outcome, error) {
	content := v.content
	if !v.humanText {
		return drop("sender"), nil
	}
	if !isString(content["msgtype"], msgtypeHuman) {
		return drop("schema"), nil
	}
	relates, isRelates, ok := relatesOf(content)
	if !ok {
		return drop("schema"), nil
	}
	if isEdit(content, relates) {
		return drop("edit"), nil
	}
	body, isText := content["body"].(string)
	if !isText {
		return drop("schema"), nil
	}
	if size, err := utf8Len(body); err != nil || size > maxHumanBodyBytes {
		return drop("size"), err
	}
	if !addressed(content, v.self) {
		return ignore("not-addressed"), nil
	}
	if _, reply := relates[inReplyTo]; isRelates && reply {
		if stripReplyFallback(body) == "" {
			return ignore("empty"), nil
		}
	}
	return outcome{kind: outcomeAccept, class: classHuman}, nil
}

// relatesOf is the content's m.relates_to: absent (ok), an object (ok), or anything else
// (not ok: schema).
func relatesOf(content pyjson.Object) (pyjson.Object, bool, bool) {
	value, present := content[relatesKey]
	if !present || value == nil || value == (pyjson.Null{}) {
		return nil, false, true
	}
	relates, isObject := value.(pyjson.Object)
	return relates, isObject, isObject
}

// isEdit is `"m.new_content" in content or relates.get("rel_type") == "m.replace"`.
func isEdit(content, relates pyjson.Object) bool {
	_, edited := content[newContent]
	return edited || isString(relates["rel_type"], relReplace)
}

// addressed is protocol._addressed.
func addressed(content pyjson.Object, self string) bool {
	mentions, ok := content[mentionsKey].(pyjson.Object)
	if !ok {
		return false
	}
	if room, isBool := mentions["room"].(bool); isBool && room {
		return true
	}
	users, ok := mentions["user_ids"].([]any)
	return ok && slices.ContainsFunc(users, func(u any) bool { return isString(u, self) })
}

// stripReplyFallback is protocol.strip_reply_fallback.
func stripReplyFallback(body string) string {
	lines := strings.Split(body, "\n")
	i := 0
	for i < len(lines) && strings.HasPrefix(lines[i], ">") {
		i++
	}
	if i == 0 {
		return body
	}
	if i < len(lines) && lines[i] == "" {
		i++
	}
	return strings.Join(lines[i:], "\n")
}

// agentTextOutcome is protocol._agent_text_outcome.
func (v *receiving) agentTextOutcome() (outcome, error) {
	if !v.namesSelf() {
		return ignore("agent-text"), nil
	}
	if !v.humanText {
		return drop("sender"), nil
	}
	err := v.validateTextContent()
	if r, ok := errors.AsType[*refusal](err); ok {
		return drop(r.reason), nil
	}
	if err != nil {
		return outcome{}, err
	}
	return outcome{kind: outcomeAccept, class: classPeer}, nil
}

// namesSelf is protocol._names.
func (v *receiving) namesSelf() bool {
	var to, users any
	if obj, ok := v.content[textKey].(pyjson.Object); ok {
		to = obj["to"]
	}
	if mentions, ok := v.content[mentionsKey].(pyjson.Object); ok {
		users = mentions["user_ids"]
	}
	return slices.ContainsFunc([]any{to, users}, func(x any) bool {
		list, ok := x.([]any)
		return ok && slices.ContainsFunc(list, func(u any) bool { return isString(u, v.self) })
	})
}

// validateTextContent is protocol.validate_text_content with check_secret False.
func (v *receiving) validateTextContent() error {
	content := v.content
	if err := versionFirst(content[textKey]); err != nil {
		return err
	}
	if err := checkTextReply(content); err != nil {
		return err
	}
	rest := slices.DeleteFunc(slices.Collect(mapsKeys(content)), func(k string) bool { return k == relatesKey })
	if !sameSet(rest, textContentKeys) || !isString(content["msgtype"], msgtypeAgent) {
		return refuse("schema")
	}
	to, body, err := textObject(content)
	if err != nil {
		return err
	}
	targets, err := v.checkTargets(to, "", maxTextTo)
	if err != nil {
		return err
	}
	if !mentionsAre(content[mentionsKey], targets) {
		return refuse("schema")
	}
	shown, isText := content["body"].(string)
	if !isText {
		return refuse("schema")
	}
	if shown != renderTag+" text -> "+strings.Join(targets, " ")+"\n"+body {
		return refuse("body")
	}
	return nil
}

// textObject is the agent_bus.text object's targets and text, their types and size checked.
func textObject(content pyjson.Object) ([]string, string, error) {
	obj, ok := content[textKey].(pyjson.Object)
	if !ok || !keysAre(obj, textObjectKeys) {
		return nil, "", refuse("schema")
	}
	n, isNumber := obj["v"].(pyjson.Number)
	to, isList := obj["to"].([]any)
	body, isText := obj["text"].(string)
	if !isNumber || n.Kind != pyjson.Int || !isList || !pingTypeOK("to", to) || !isText || body == "" {
		return nil, "", refuse("schema")
	}
	size, err := utf8Len(body)
	if err != nil {
		return nil, "", err
	}
	if size > maxAgentTextBytes {
		return nil, "", refuse("size")
	}
	return texts(to), body, nil
}

// listValue is a value already checked to be a list.
func listValue(value any) []any {
	if list, ok := value.([]any); ok {
		return list
	}
	return nil
}

// checkTextReply is protocol._check_text_reply.
func checkTextReply(content pyjson.Object) error {
	if _, edited := content[newContent]; edited {
		return refuse("edit")
	}
	value, present := content[relatesKey]
	if !present {
		return nil
	}
	relates, isObject := value.(pyjson.Object)
	if isObject && isString(relates["rel_type"], relReplace) {
		return refuse("edit")
	}
	if !isObject || !keysAre(relates, []string{inReplyTo}) {
		return refuse("schema")
	}
	reply, isObject := relates[inReplyTo].(pyjson.Object)
	if !isObject || !keysAre(reply, []string{"event_id"}) || !isEventID(reply["event_id"]) {
		return refuse("schema")
	}
	return nil
}

// questionsOutcome is protocol._questions_outcome: in the questions room only a coordinator
// receives, and only a human's message.
func (v *receiving) questionsOutcome(class string) (outcome, error) {
	if v.record.roles[v.self] != roleCoordinator {
		return ignore("not-coordinator"), nil
	}
	role, err := v.roleOf(v.sender)
	if err != nil {
		return outcome{}, err
	}
	switch {
	case role == roleCoordinator:
		return ignore("coordinator-post"), nil
	case class != senderHuman || !v.humanText:
		return drop("sender"), nil
	case !isString(v.content["msgtype"], msgtypeHuman):
		return drop("schema"), nil
	}
	body, revises, found := v.questionBody()
	if found.kind != "" {
		return found, nil
	}
	text, isText := body.(string)
	if !isText {
		return drop("schema"), nil
	}
	if size, err := utf8Len(text); err != nil || size > maxHumanBodyBytes {
		return drop("size"), err
	}
	relates, isRelates, _ := relatesOf(v.content)
	if _, reply := relates[inReplyTo]; !revises && isRelates && reply {
		text = stripReplyFallback(text)
	}
	if text == "" {
		return ignore("empty"), nil
	}
	return outcome{kind: outcomeAccept, class: classHuman}, nil
}

// questionBody is the body a questions-room message carries: its own, or an edit's new one
// (revises). A drop is returned as the outcome when the message is malformed.
func (v *receiving) questionBody() (any, bool, outcome) {
	content := v.content
	relates, isRelates, ok := relatesOf(content)
	if !ok {
		return nil, false, drop("schema")
	}
	if !isEdit(content, relates) {
		return content["body"], false, outcome{}
	}
	fresh, isObject := content[newContent].(pyjson.Object)
	if !isRelates || !isString(relates["rel_type"], relReplace) || !isEventID(relates["event_id"]) ||
		!isObject || !isString(fresh["msgtype"], msgtypeHuman) {
		return nil, false, drop("schema")
	}
	return fresh["body"], true, outcome{}
}

func mapsKeys(object pyjson.Object) func(func(string) bool) {
	return func(yield func(string) bool) {
		for key := range object {
			if !yield(key) {
				return
			}
		}
	}
}

func sameSet(got, want []string) bool {
	return len(got) == len(want) && !slices.ContainsFunc(want, func(k string) bool { return !slices.Contains(got, k) })
}
