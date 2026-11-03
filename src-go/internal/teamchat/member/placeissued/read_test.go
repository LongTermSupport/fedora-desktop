package placeissued

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"slices"
	"strings"
	"testing"
	"testing/iotest"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/exits"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/seat"
)

func TestReadTakesTheBareIssuedDocument(t *testing.T) {
	is, err := Read(bytes.NewReader(passwordDoc(t, secretOne)))
	if err != nil {
		t.Fatal(err)
	}
	if is.Kind != seat.KindPassword || is.Secret.Reveal() != secretOne {
		t.Errorf("credential kind %s", is.Kind)
	}
	if want := bundleAt(t, testBase); is.Seat != want {
		t.Errorf("seat %+v\nwant %+v", is.Seat, want)
	}
}

func TestReadTakesTheAnswerOfAMemberVerb(t *testing.T) {
	bare := passwordDoc(t, secretOne)
	want, err := seat.DecodeIssued(bare)
	if err != nil {
		t.Fatal(err)
	}
	cases := map[string][]byte{
		"handle, changed and bundle": answerDoc(t, bare, nil),
		"with notes":                 answerDoc(t, bare, map[string]any{"notes": []string{"a note"}}),
		"without a handle":           answerDoc(t, bare, map[string]any{"handle": nil}),
		"with a trailing newline":    append(answerDoc(t, bare, nil), '\n'),
		"in indented JSON":           append([]byte("\n  "), append(answerDoc(t, bare, nil), " \n\t"...)...),
	}
	for name, doc := range cases {
		is, err := Read(bytes.NewReader(doc))
		if err != nil {
			t.Errorf("%s: %v", name, err)
			continue
		}
		if is != want {
			t.Errorf("%s: issued %+v, want the bundle's", name, is.Seat)
		}
	}
}

func TestReadTakesEitherCredentialKindInEitherShape(t *testing.T) {
	for _, kind := range []seat.Kind{seat.KindPassword, seat.KindToken} {
		bare := issuedDoc(t, testBase, kind, secretTwo)
		for name, doc := range map[string][]byte{"bare": bare, "answer": answerDoc(t, bare, nil)} {
			is, err := Read(bytes.NewReader(doc))
			if err != nil {
				t.Errorf("%s %s: %v", kind, name, err)
				continue
			}
			if is.Kind != kind || is.Secret.Reveal() != secretTwo {
				t.Errorf("%s %s: read a %s", kind, name, is.Kind)
			}
		}
	}
}

// padded is doc followed by spaces to exactly size bytes.
func padded(t *testing.T, doc []byte, size int) []byte {
	t.Helper()
	if len(doc) > size {
		t.Fatalf("a %d byte document is over %d", len(doc), size)
	}
	return append(bytes.Clone(doc), bytes.Repeat([]byte(" "), size-len(doc))...)
}

func TestReadTakesADocumentOfExactlyTheCap(t *testing.T) {
	if _, err := Read(bytes.NewReader(padded(t, passwordDoc(t, secretOne), maxDocument))); err != nil {
		t.Fatal(err)
	}
}

// counting is a reader of a fixed text that counts the bytes it was asked for, so a read with no
// cap shows as more than the cap without a test that never ends.
type counting struct {
	r    io.Reader
	read int
}

func (c *counting) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	c.read += n
	return n, err
}

func TestReadReadsOneByteOverTheCapAndRefuses(t *testing.T) {
	text := padded(t, passwordDoc(t, secretOne), maxDocument+4096)
	in := &counting{r: bytes.NewReader(text)}
	_, err := Read(in)
	if !errors.Is(err, ErrTooBig) {
		t.Fatalf("err %v, want ErrTooBig", err)
	}
	if in.read != maxDocument+1 {
		t.Errorf("read %d bytes, want the cap and one more (%d)", in.read, maxDocument+1)
	}
}

func TestReadPassesOnAReadError(t *testing.T) {
	boom := errors.New("the pipe broke")
	_, err := Read(io.MultiReader(strings.NewReader(`{"seat"`), iotest.ErrReader(boom)))
	if !errors.Is(err, boom) {
		t.Fatalf("err %v, want the read error", err)
	}
	if refusal, isRefusal := errors.AsType[*FailureError](err); isRefusal {
		t.Errorf("a read error is the machine's, not a refusal of the document: %v", refusal)
	}
}

// refusal is one document Read refuses: the sentinel it is, the message of its error object,
// and the text its cause line must hold ("" when it has none).
type refusal struct {
	name  string
	doc   []byte
	is    error
	msg   exits.Template
	cause string
}

func refusals(t *testing.T) []refusal {
	t.Helper()
	bare := passwordDoc(t, secretOne)
	answer := func(more map[string]any) []byte { return answerDoc(t, bare, more) }
	badSeat := func(f func(seatObj map[string]any)) []byte {
		return edit(t, bare, func(_, seatObj map[string]any) { f(seatObj) })
	}
	badTop := func(f func(top map[string]any)) []byte {
		return edit(t, bare, func(top, _ map[string]any) { f(top) })
	}
	notAnIssued := []refusal{
		{"nothing at all", nil, ErrEmpty, msgEmpty, ""},
		{"only white space", []byte(" \n\t "), ErrEmpty, msgEmpty, ""},
		{"over the cap", padded(t, bare, maxDocument+1), ErrTooBig, msgTooBig, ""},
		{"not JSON", []byte("this is not json " + secretOne), ErrNotJSON, msgNotJSON, "strict JSON"},
		{"cut short", bare[:len(bare)/2], ErrNotJSON, msgNotJSON, "strict JSON"},
		{"a repeated key", []byte(`{"seat":{},"seat":{},"password":"` + secretOne + `"}`), ErrNotJSON, msgNotJSON, "repeated"},
		{"bytes after the document", append(bytes.Clone(bare), " x"...), ErrTrailing, msgTrailing, "content after the value"},
		{"two documents", append(bytes.Clone(bare), bare...), ErrTrailing, msgTrailing, "content after the value"},
		{"an array", []byte(`["` + secretOne + `"]`), ErrNotIssued, msgNotIssued, ""},
		{"a string", []byte(`"` + secretOne + `"`), ErrNotIssued, msgNotIssued, ""},
		{"an object with neither a seat nor a bundle", []byte(`{"password":"` + secretOne + `"}`), ErrNotIssued, msgNotIssued, ""},
	}
	notAnAnswer := []refusal{
		{"an answer with a member of its own", answer(map[string]any{"extra": 1}), ErrEnvelope, msgEnvelope, ""},
		{"an answer that is also a seat", answer(map[string]any{"seat": map[string]any{}}), ErrEnvelope, msgEnvelope, ""},
		{"an answer of another version", answer(map[string]any{"v": 2}), ErrEnvelope, msgEnvelope, ""},
		{"an answer whose version is text", answer(map[string]any{"v": "1"}), ErrEnvelope, msgEnvelope, ""},
		{"an answer with no version", answer(map[string]any{"v": nil}), ErrEnvelope, msgEnvelope, ""},
		{"an answer with no changes", answer(map[string]any{"changed": nil}), ErrEnvelope, msgEnvelope, ""},
		{"changes that are not an array", answer(map[string]any{"changed": "x"}), ErrEnvelope, msgEnvelope, ""},
		{"changes that are not all text", answer(map[string]any{"changed": []any{"x", 1}}), ErrEnvelope, msgEnvelope, ""},
		{"a handle that is not text", answer(map[string]any{"handle": 3}), ErrEnvelope, msgEnvelope, ""},
		{"notes that are not an array", answer(map[string]any{"notes": "x"}), ErrEnvelope, msgEnvelope, ""},
		{"notes that are not all text", answer(map[string]any{"notes": []any{1}}), ErrEnvelope, msgEnvelope, ""},
		{"a bundle that is not an object", answer(map[string]any{"bundle": secretOne}), ErrEnvelope, msgEnvelope, ""},
	}
	notAValidSeat := []refusal{
		{"an answer whose bundle is an answer", answer(map[string]any{"bundle": json.RawMessage(answer(nil))}), seat.ErrInvalid, msgInvalid, "not an object of a seat and one credential"},
		{"a member besides the seat and the credential", badTop(func(top map[string]any) { top["extra"] = 1 }), seat.ErrInvalid, msgInvalid, "not an object of a seat and one credential"},
		{"both credentials", badTop(func(top map[string]any) { top["token"] = secretTwo }), seat.ErrInvalid, msgInvalid, "not an object of a seat and one credential"},
		{"a seat with no credential", badTop(func(top map[string]any) { delete(top, "password"); top["other"] = secretOne }), seat.ErrInvalid, msgInvalid, "no password or token"},
		{"a credential that is not text", badTop(func(top map[string]any) { top["password"] = 7 }), seat.ErrInvalid, msgInvalid, "not a string"},
		{"an empty credential", badTop(func(top map[string]any) { top["password"] = "" }), seat.ErrInvalid, msgInvalid, "credential"},
		{"a credential with a space", badTop(func(top map[string]any) { top["password"] = secretOne + " " + secretTwo }), seat.ErrInvalid, msgInvalid, "credential"},
		{"an unknown field in the seat", badSeat(func(s map[string]any) { s["leakmarker"] = secretOne }), seat.ErrInvalid, msgInvalid, "seat: invalid"},
		{"a bundle of version 1", badSeat(func(s map[string]any) { s["v"] = 1; delete(s, "tls_ca"); s["plain_http_hosts"] = []string{} }), seat.ErrInvalid, msgInvalid, "v is 1, this build reads only v 2"},
		{"https with no CA key", badSeat(func(s map[string]any) { s["base_url"] = "https://chat.example.test:8448"; delete(s, "tls_ca") }), seat.ErrInvalid, msgInvalid, "no tls_ca"},
		{"https with an empty CA", badSeat(func(s map[string]any) { s["base_url"] = "https://chat.example.test:8448"; s["tls_ca"] = "" }), seat.ErrInvalid, msgInvalid, "base_url and tls_ca are not a member URL"},
		{"plain http beyond loopback", badSeat(func(s map[string]any) { s["base_url"] = "http://192.0.2.7:8448" }), seat.ErrInvalid, msgInvalid, "base_url and tls_ca are not a member URL"},
		{"a server name that is not the team's", badSeat(func(s map[string]any) { s["server_name"] = "other.example.test" }), seat.ErrInvalid, msgInvalid, "server_name is not the team's"},
	}
	return slices.Concat(notAnIssued, notAnAnswer, notAValidSeat)
}

func TestReadRefusesWhatIsNotAnIssuedSeat(t *testing.T) {
	for _, c := range refusals(t) {
		_, err := Read(bytes.NewReader(c.doc))
		if !errors.Is(err, c.is) {
			t.Errorf("%s: err %v, want %v", c.name, err, c.is)
			continue
		}
		f := failureOf(t, err)
		if f.Object.Exit != exits.ExitConfig || f.Object.Code != exits.CodeBundle || f.Object.Message != string(c.msg) {
			t.Errorf("%s: object %+v, want 78 bundle %q", c.name, *f.Object, c.msg)
		}
		if !strings.Contains(f.Cause, c.cause) {
			t.Errorf("%s: cause %q does not hold %q", c.name, f.Cause, c.cause)
		}
		if c.cause == "" && f.Cause != "" {
			t.Errorf("%s: cause %q, want none", c.name, f.Cause)
		}
	}
}

func TestReadRefusalsShowNoDocumentText(t *testing.T) {
	for _, c := range refusals(t) {
		_, err := Read(bytes.NewReader(c.doc))
		code, stderr, obj := reported(t, err)
		if code != exits.ExitConfig || obj.Code != exits.CodeBundle {
			t.Errorf("%s: exit %d object %+v", c.name, code, obj)
		}
		for _, leaked := range []string{secretOne, secretTwo, "leakmarker"} {
			if strings.Contains(stderr, leaked) || strings.Contains(err.Error(), leaked) {
				t.Errorf("%s: the output shows document text %q:\n%s", c.name, leaked, stderr)
			}
		}
	}
}
