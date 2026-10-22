package proto

import (
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"
)

// RefusalError is a send the sender refuses (A 7.6's closed set of send refusals). Detail says what
// is wrong for the session; it never quotes the refused text, which may hold a secret.
type RefusalError struct {
	Reason, Detail string
}

func (r *RefusalError) Error() string {
	if r.Detail == "" {
		return "teamchat: refused " + r.Reason
	}
	return "teamchat: refused " + r.Reason + ": " + r.Detail
}

func refused(reason, detail string) *RefusalError {
	return &RefusalError{Reason: reason, Detail: detail}
}

// MaxOptions is the most `Options:` lines a question has (P54).
const MaxOptions = 9

// The sections of a question's form (A 10.2).
const (
	SectionContext  = "Context:"
	SectionQuestion = "Question:"
	SectionOptions  = "Options:"
)

var (
	questionSections = []string{SectionContext, SectionQuestion, SectionOptions}
	optionRe         = regexp.MustCompile(`^([1-9])\.\s`)
)

// Headline is the first line of a report or question, trimmed: 1 to MaxHeadline code points,
// else `body` (A 10.1, 10.2).
func Headline(text string) (string, error) {
	headline := strings.TrimSpace(strings.SplitN(text, "\n", 2)[0])
	if headline == "" || utf8.RuneCountInString(headline) > MaxHeadline {
		return "", refused(ReasonBody, "the first line (the headline) must be 1 to 120 characters")
	}
	return headline, nil
}

// ReportForm is a report's text (A 10.1): at most MaxReportBody bytes, never cut, and a
// headline. It gives the headline.
func ReportForm(text string) (string, error) {
	if len(text) > MaxReportBody {
		return "", refused(ReasonBody, "a report is at most 1500 bytes")
	}
	return Headline(text)
}

// QuestionForm is a question's body (A 10.2, P54): `Q<n> `, then a headline, the `Context:` and
// `Question:` sections, an optional `Options:` numbered `1.` to `9.` in order, at most
// MaxQuestionBody bytes in all. It gives the headline and the `Question:` section; the
// sender and the question index read the same form.
func QuestionForm(n int64, body string) (string, string, error) {
	text, numbered := strings.CutPrefix(body, QuestionName(int(n))+" ")
	switch {
	case !numbered:
		return "", "", refused(ReasonBody, "the body starts Q<n> and a space")
	case len(body) > MaxQuestionBody:
		return "", "", refused(ReasonBody, "a question is at most 8000 bytes")
	}
	headline, err := Headline(text)
	if err != nil {
		return "", "", err
	}
	if sectionOf(headline) != "" {
		return "", "", refused(ReasonBody, "the first line is the headline, before the Context: section")
	}
	sections, err := questionSectionsOf(text)
	if err != nil {
		return "", "", err
	}
	for _, name := range []string{SectionContext, SectionQuestion} {
		if sections[name] == "" {
			return "", "", refused(ReasonBody, "the "+name+" section is missing")
		}
	}
	if options, ok := sections[SectionOptions]; ok && !validOptions(options) {
		return "", "", refused(ReasonBody, "Options: are lines numbered 1. to 9., in order")
	}
	return headline, sections[SectionQuestion], nil
}

// ThreadBody is a thread post's body (A 10.3): at most MaxThreadBody bytes, not blank unless
// it closes the question, whose blank body is `Q<n> closed.`.
func ThreadBody(n int64, text string, closes bool) (string, error) {
	switch {
	case len(text) > MaxThreadBody:
		return "", refused(ReasonBody, "a thread post is at most 4000 bytes")
	case strings.TrimSpace(text) != "":
		return text, nil
	case closes:
		return QuestionName(int(n)) + " closed.", nil
	}
	return "", refused(ReasonBody, "a thread post that does not close its question has text")
}

func sectionOf(line string) string {
	for _, name := range questionSections {
		if strings.HasPrefix(line, name) {
			return name
		}
	}
	return ""
}

// questionSectionsOf is each section from its line to the next section's, trimmed; a section
// named twice is `body`.
func questionSectionsOf(text string) (map[string]string, error) {
	found := map[string][]string{}
	current := ""
	for _, line := range strings.Split(text, "\n")[1:] {
		if name := sectionOf(line); name != "" {
			if _, twice := found[name]; twice {
				return nil, refused(ReasonBody, "the "+name+" section is given twice")
			}
			current, found[name] = name, []string{strings.TrimPrefix(line, name)}
		} else if current != "" {
			found[current] = append(found[current], line)
		}
	}
	sections := make(map[string]string, len(found))
	for name, lines := range found {
		sections[name] = strings.TrimSpace(strings.Join(lines, "\n"))
	}
	return sections, nil
}

// validOptions: the non-blank lines are `1. ` to at most `9. `, in order.
func validOptions(options string) bool {
	n := 0
	for line := range strings.SplitSeq(options, "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		n++
		m := optionRe.FindStringSubmatch(line + " ")
		if m == nil || m[1] != strconv.Itoa(n) {
			return false
		}
	}
	return n >= 1 && n <= MaxOptions
}

// collapseSpace is text with each run of white space one space, trimmed: the form A 10.2's
// duplicate check compares.
func collapseSpace(text string) string { return strings.Join(strings.Fields(text), " ") }
