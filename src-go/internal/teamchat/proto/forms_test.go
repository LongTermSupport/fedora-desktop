package proto

import (
	"errors"
	"strings"
	"testing"
)

func refusalOf(err error) string {
	if r, ok := errors.AsType[*RefusalError](err); ok {
		return r.Reason
	}
	return ""
}

const goodQuestion = "Which licence for the docs site?\nContext: the site goes public.\nQuestion: MIT or CC-BY?\nOptions:\n1. MIT\n2. CC-BY-4.0"

func TestRefusalNamesReasonAndDetail(t *testing.T) {
	err := refused(ReasonBody, "the Context: section is missing")
	if err.Error() != "teamchat: refused body: the Context: section is missing" {
		t.Errorf("Error() = %q", err.Error())
	}
	if refused(ReasonRate, "").Error() != "teamchat: refused rate" {
		t.Errorf("a refusal without detail: %q", refused(ReasonRate, "").Error())
	}
}

func TestHeadline(t *testing.T) {
	cases := map[string]string{
		"Merged the parser fix\nmore":        "Merged the parser fix",
		"  padded  ":                         "padded",
		strings.Repeat("é", MaxHeadline):     strings.Repeat("é", MaxHeadline),
		"x" + strings.Repeat(" ", 200) + "y": "",
		"":                                   "",
		"   \nbody":                          "",
		strings.Repeat("a", MaxHeadline+1):   "",
	}
	for text, want := range cases {
		got, err := Headline(text)
		if want == "" {
			if refusalOf(err) != ReasonBody {
				t.Errorf("Headline(%.20q) = %q, %v; want refused body", text, got, err)
			}
			continue
		}
		if err != nil || got != want {
			t.Errorf("Headline(%.20q) = %q, %v; want %q", text, got, err, want)
		}
	}
}

func TestReportForm(t *testing.T) {
	if h, err := ReportForm("Merged\nPR 12 is in main."); err != nil || h != "Merged" {
		t.Errorf("ReportForm = %q, %v", h, err)
	}
	if _, err := ReportForm("x\n" + strings.Repeat("a", MaxReportBody)); refusalOf(err) != ReasonBody {
		t.Errorf("an over-long report: %v", err)
	}
	if _, err := ReportForm("\nno headline"); refusalOf(err) != ReasonBody {
		t.Errorf("a blank headline: %v", err)
	}
}

func TestQuestionForm(t *testing.T) {
	h, q, err := QuestionForm(3, "Q3 "+goodQuestion)
	if err != nil || h != "Which licence for the docs site?" || q != "MIT or CC-BY?" {
		t.Fatalf("QuestionForm(good) = %q, %q, %v", h, q, err)
	}
	multi := "Q1 Head\nContext: a\nb\nQuestion:  first\n  second  \n"
	if _, q, err := QuestionForm(1, multi); err != nil || q != "first\n  second" {
		t.Errorf("a multi-line section = %q, %v", q, err)
	}
	if _, _, err := QuestionForm(1, "Q1 Head\nContext: a\nQuestion: b\nOptions:\n1."); err != nil {
		t.Errorf("an option with no text: %v", err)
	}
}

func TestQuestionFormRefuses(t *testing.T) {
	cases := map[string]string{
		"no prefix":         goodQuestion,
		"wrong number":      "Q4 " + goodQuestion,
		"over the cap":      "Q3 " + goodQuestion + "\n" + strings.Repeat("a", MaxQuestionBody),
		"headline section":  "Q3 Context: a\nQuestion: b",
		"no context":        "Q3 Head\nQuestion: b",
		"empty question":    "Q3 Head\nContext: a\nQuestion:   ",
		"twice":             "Q3 Head\nContext: a\nQuestion: b\nContext: c",
		"options gap":       "Q3 Head\nContext: a\nQuestion: b\nOptions:\n1. x\n3. y",
		"options unnumered": "Q3 Head\nContext: a\nQuestion: b\nOptions:\nfree text",
		"options empty":     "Q3 Head\nContext: a\nQuestion: b\nOptions:",
		"options ten":       "Q3 Head\nContext: a\nQuestion: b\nOptions:\n" + options(10),
		"blank headline":    "Q3 \nContext: a\nQuestion: b",
	}
	for name, body := range cases {
		if _, _, err := QuestionForm(3, body); refusalOf(err) != ReasonBody {
			t.Errorf("%s: %v, want refused body", name, err)
		}
	}
	if _, _, err := QuestionForm(3, "Q3 Head\nContext: a\nQuestion: b\nOptions:\n"+options(9)); err != nil {
		t.Errorf("nine options: %v", err)
	}
}

func options(n int) string {
	lines := make([]string, 0, n)
	for i := 1; i <= n; i++ {
		lines = append(lines, string(rune('0'+i%10))+". option")
	}
	return strings.Join(lines, "\n")
}

func TestThreadForm(t *testing.T) {
	cases := []struct {
		text   string
		closes bool
		body   string
		reason string
	}{
		{"Relayed to the worker.", false, "Relayed to the worker.", ""},
		{"", true, "Q3 closed.", ""},
		{"  \n", true, "Q3 closed.", ""},
		{"Thanks.", true, "Thanks.", ""},
		{"", false, "", ReasonBody},
		{" \t", false, "", ReasonBody},
		{strings.Repeat("a", MaxThreadBody+1), false, "", ReasonBody},
	}
	for _, tc := range cases {
		body, err := ThreadBody(3, tc.text, tc.closes)
		if refusalOf(err) != tc.reason || body != tc.body {
			t.Errorf("ThreadBody(%.20q, %v) = %q, %v; want %q %q", tc.text, tc.closes, body, err, tc.body, tc.reason)
		}
	}
}

func TestCollapseSpace(t *testing.T) {
	if got := collapseSpace("  MIT\n or\tCC-BY? "); got != "MIT or CC-BY?" {
		t.Errorf("collapseSpace = %q", got)
	}
}
