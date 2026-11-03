package proto

import (
	"strconv"
	"strings"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/pingbus/pyjson"
)

// The content keys and message types of the side rooms (Plan 00211 SPEC §7-§8).
const (
	KeyReport        = "agent_bus.report"
	KeyQuestion      = "agent_bus.question"
	KeyQuestionState = "agent_bus.question_state"
	noticeMsgtype    = "m.notice"
	textMsgtype      = "m.text"
	mentionsKey      = "m.mentions"
	bodyKey          = "body"
	msgtypeKey       = "msgtype"
	eventIDKey       = "event_id"
	threadRelation   = "m.thread"
)

// The side rooms' text limits.
const (
	headlineMax      = 120
	maxReportBytes   = 1500
	maxThreadBytes   = 4000
	maxQuestionBytes = 8000
)

func refuseBody(detail string) error { return &RefusalError{Reason: ReasonBody, Detail: detail} }

// withinBytes is a text that has a UTF-8 form of at most limit bytes; a lone surrogate has
// none (D10).
func withinBytes(text string, limit int) bool {
	size, ok := utf8Size(text)
	return ok && size <= limit
}

// firstLine is the text before the first newline, or all of it.
func firstLine(text string) string {
	return strings.SplitN(text, "\n", 2)[0]
}

// checkHeadline is the first line of a report or question: not blank, at most 120 characters.
func checkHeadline(text string) (string, error) {
	headline := firstLine(text)
	if pyStrip(headline) == "" || charLen(headline) > headlineMax {
		return "", refuseBody("the first line (the headline) must be 1 to " + strconv.Itoa(headlineMax) + " characters")
	}
	return headline, nil
}

// CheckReportText is SPEC P51: a headline, an optional body, at most 1500 bytes in all, no
// secret's shape. It returns the headline.
func CheckReportText(text string) (string, error) {
	if !withinBytes(text, maxReportBytes) {
		return "", refuseBody("a report is at most " + strconv.Itoa(maxReportBytes) + " bytes")
	}
	headline, err := checkHeadline(text)
	if err != nil {
		return "", err
	}
	if SecretShape(text) != "" {
		return "", refuse(RefuseSecret)
	}
	return headline, nil
}

// CheckThreadText is SPEC P52: a coordinator's thread post, at most 4000 bytes, not empty
// unless it closes the question, no secret's shape.
func CheckThreadText(text string, closing bool) error {
	if !withinBytes(text, maxThreadBytes) {
		return refuseBody("a thread post is at most " + strconv.Itoa(maxThreadBytes) + " bytes")
	}
	if text == "" && !closing {
		return refuseBody("the post's text is on stdin")
	}
	if SecretShape(text) != "" {
		return refuse(RefuseSecret)
	}
	return nil
}

// BuildReport is SPEC P53: a notice with no mentions, so Element never notifies it.
func BuildReport(text string) pyjson.Object {
	return pyjson.Object{
		msgtypeKey: noticeMsgtype, bodyKey: text, mentionsKey: pyjson.Object{},
		KeyReport: pyjson.Object{"v": intNum(1)},
	}
}

func isString(v any) bool {
	switch v.(type) {
	case string:
		return true
	default:
		return false
	}
}

// CheckReportContent is SPEC P53: a RefusalError (schema) unless the content is exactly a
// report's shape.
func CheckReportContent(content any) error {
	obj, ok := content.(pyjson.Object)
	if !ok || !hasExactKeys(obj, msgtypeKey, bodyKey, mentionsKey, KeyReport) {
		return refuse(ReasonSchema)
	}
	mentions, isObj := obj[mentionsKey].(pyjson.Object)
	if obj[msgtypeKey] != noticeMsgtype || !isObj || len(mentions) != 0 || !isString(obj[bodyKey]) ||
		pyjson.Canonical(obj[KeyReport]) != pyjson.Canonical(pyjson.Object{"v": intNum(1)}) {
		return refuse(ReasonSchema)
	}
	return nil
}
