package proto

import (
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/richtext"
)

// formattedAs fails unless content carries body's HTML as its `formatted_body` (Plan 00241
// Phase 3): `format` org.matrix.custom.html, and the plain body unchanged.
func formattedAs(t *testing.T, name string, content Value, body string) {
	t.Helper()
	if got := at(t, content, "body").Str(); got != body {
		t.Errorf("%s: body %q, want the markdown %q", name, got, body)
	}
	if got := at(t, content, keyFormat).Str(); got != richtext.Format {
		t.Errorf("%s: format %q", name, got)
	}
	if got, want := at(t, content, keyFormattedBody).Str(), richtext.HTML(body); got != want {
		t.Errorf("%s: formatted_body %q, want %q", name, got, want)
	}
}

func plain(t *testing.T, name string, content Value) {
	t.Helper()
	for _, key := range []string{keyFormat, keyFormattedBody} {
		if _, ok := content.Get(key); ok {
			t.Errorf("%s: %s present in %s", name, key, content.Compact())
		}
	}
}

// TestComposeFormatsWhatPeopleRead: reports, questions, thread posts and agent text carry
// their body as HTML too, and every receiver still delivers them.
func TestComposeFormatsWhatPeopleRead(t *testing.T) {
	report := "## Merged the parser fix\n\n| PR | State |\n|---|---|\n| 12 | **merged** |"
	out := mustSend(t)(composer(t, workerID).Report(report))
	formattedAs(t, "report", out.Content, report)
	if v := receive(t, out, workerID, coordID); v.Reason != "" {
		t.Errorf("a formatted report is dropped %q", v.Reason)
	}

	c := composer(t, coordID)
	out = mustSend(t)(c.Question(QuestionPost{Text: goodQuestion, AskedBy: []string{workerID}}))
	formattedAs(t, "question", out.Content, "Q1 "+goodQuestion)
	if v := receive(t, out, coordID, coordID); v.Outcome != Index {
		t.Errorf("a formatted question is not indexed: %v %q", v.Outcome, v.Reason)
	}

	out = mustSend(t)(asked(t).Thread(ThreadPost{N: 1, Text: "Relayed:\n\n- MIT\n- `CC-BY`"}))
	formattedAs(t, "thread", out.Content, "Relayed:\n\n- MIT\n- `CC-BY`")

	text := "Branch pushed: *see* <b>this</b>"
	out = mustSend(t)(composer(t, workerID).Text(Text{To: []string{aliceID}, Text: text}, "", text))
	formattedAs(t, "text", out.Content, text)
	if v := receive(t, out, workerID, coordID); v.Reason != "" {
		t.Errorf("formatted text is dropped %q", v.Reason)
	}
}

// TestComposeKeepsGeneratedPostsPlain: pings, notices and carried items are generated lines,
// not an agent's markdown, and stay plain.
func TestComposeKeepsGeneratedPostsPlain(t *testing.T) {
	out := mustSend(t)(composer(t, workerID).Ping(Ping{Verb: "review", To: []string{coordID}, Ref: commitRef}, "", "review *this*"))
	plain(t, "ping", out.Content)
}

// TestComposeDropsHTMLThatDoesNotFit: the HTML is display only, so a body whose HTML would
// take the content past MaxContent is sent with its plain body alone, never refused for it.
func TestComposeDropsHTMLThatDoesNotFit(t *testing.T) {
	text := strings.Repeat("<", MaxText)
	out := mustSend(t)(composer(t, workerID).Text(Text{To: []string{coordID}, Text: text}, "", text))
	plain(t, "oversized text", out.Content)
	if out.Content.Size() > MaxContent {
		t.Errorf("content is %d bytes", out.Content.Size())
	}
}

// TestDecodeContentFormat: a receiver takes `format` only as the pair, org.matrix.custom.html
// with a string `formatted_body`, and never reads the HTML.
func TestDecodeContentFormat(t *testing.T) {
	text := func(extra string) string {
		return `{"body":"x"` + extra + `,"teamchat.text":{"v":1,"to":["` + aliceID + `"],"text":"x"}}`
	}
	cases := map[string]struct{ json, want string }{
		"pair":                 {text(`,"format":"org.matrix.custom.html","formatted_body":"<p>x</p>"`), ""},
		"format alone":         {text(`,"format":"org.matrix.custom.html"`), ReasonSchema},
		"formatted_body alone": {text(`,"formatted_body":"<p>x</p>"`), ReasonSchema},
		"other format":         {text(`,"format":"text/markdown","formatted_body":"x"`), ReasonSchema},
		"format not string":    {text(`,"format":1,"formatted_body":"x"`), ReasonSchema},
		"formatted not string": {text(`,"format":"org.matrix.custom.html","formatted_body":1`), ReasonSchema},
	}
	for name, tc := range cases {
		if _, reason := decode(t, MainKeys, tc.json); reason != tc.want {
			t.Errorf("%s: reason %q, want %q", name, reason, tc.want)
		}
	}
}
