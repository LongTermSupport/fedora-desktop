package placeissued

import (
	"errors"
	"io"
	"regexp"
	"strings"
	"unicode"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/exits"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

// The messages of the error objects, each fixed text: a message quotes nothing of the document
// and no credential (B 2.2.4). What a refusal cannot say in a fixed message travels in the cause
// line of its FailureError.
const (
	msgEmpty       exits.Template = "nothing on stdin; pipe in the answer of teamchat-admin member add, return or reissue"
	msgTooBig      exits.Template = "the document on stdin is larger than an issued seat can be"
	msgNotJSON     exits.Template = "the document on stdin is not strict JSON"
	msgTrailing    exits.Template = "the document on stdin has content after its JSON value"
	msgNotIssued   exits.Template = "the document on stdin is neither an issued seat nor the answer of a member verb"
	msgEnvelope    exits.Template = "the answer on stdin is not the answer of a member verb: its members are not v, changed, handle, notes and bundle, or are of the wrong type"
	msgInvalid     exits.Template = "the issued seat on stdin is not valid for this seat store; the line above says why"
	msgClash       exits.Template = "the issued seat is for a team that this checkout has on another homeserver; the line above names both"
	msgUnreachable exits.Template = "the homeserver at {} did not answer from this machine; the seat was not placed"
	msgBusy        exits.Template = "another run holds the allocation lock of this checkout; try again"
	msgHeld        exits.Template = "the seat is held by a session; release it first"
	msgStore       exits.Template = "the seat store is not as the owner must have it; the line above says which part"
)

// FailureError is a refusal of place-issued: the error object that ends stderr, and a cause line
// written before it. The cause carries what the fixed message cannot: the rule a document broke,
// the two homeservers of a clash. Report writes it as one line with no quoted text.
type FailureError struct {
	// Object is the error object, always one of exit 20, 75 or 78.
	Object *exits.Error
	// Cause is the text of the line before the object; empty for none.
	Cause string
	err   error
}

// Error is the object's message.
func (f *FailureError) Error() string { return f.Object.Message }

// Unwrap is the error object and what caused the refusal, so errors.Is finds the sentinel and
// exits.Write finds the object.
func (f *FailureError) Unwrap() []error {
	out := []error{f.Object}
	if f.err != nil {
		out = append(out, f.err)
	}
	return out
}

// Report writes err to stderr as the member binary does (exits.Write): a FailureError as its cause
// line, when it has one, then its error object last; any other error as exits.Write prints an
// unknown one. It answers the exit status.
func Report(stderr io.Writer, err error) term.ExitCode {
	f, ok := errors.AsType[*FailureError](err)
	if !ok {
		return exits.Write(stderr, err)
	}
	if f.Cause != "" {
		if _, werr := io.WriteString(stderr, "teamchat: "+diagnostic(f.Cause)+"\n"); werr != nil {
			return f.Object.Exit
		}
	}
	return exits.Write(stderr, f.Object)
}

// quoted is a span in double quotes: how encoding/json names a field it refused, and a place a
// library might show document text.
var quoted = regexp.MustCompile(`"[^"]*"`)

// diagnostic is cause as one line: any quoted span shown as "?", any control character as a
// space.
func diagnostic(cause string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, quoted.ReplaceAllString(cause, `"?"`))
}

// refused is the FailureError of a document that is not an issued seat: 78 bundle with the fixed
// message, the sentinel for errors.Is, and the cause line (none when empty).
func refused(sentinel error, msg exits.Template, cause string) *FailureError {
	return &FailureError{Object: exits.New(exits.CodeBundle, nil, msg), Cause: cause, err: sentinel}
}
