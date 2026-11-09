package placeissued

import (
	"bytes"
	"errors"
	"fmt"
	"io"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/proto"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/seat"
)

// The ways a document on stdin is refused before it is judged as a seat; each is what
// errors.Is finds in the FailureError Read answers. A document that is not a valid seat is
// seat.ErrInvalid.
var (
	ErrEmpty     = errors.New("placeissued: nothing on stdin")
	ErrTooBig    = errors.New("placeissued: the document is over the cap")
	ErrNotJSON   = errors.New("placeissued: the document is not strict JSON")
	ErrTrailing  = errors.New("placeissued: content after the JSON value")
	ErrNotIssued = errors.New("placeissued: neither an issued seat nor the answer of a member verb")
	ErrEnvelope  = errors.New("placeissued: not the answer of a member verb")
)

const (
	// envelopeAllowance is what the answer of a member verb may hold beyond its bundle: the
	// changes it lists, the notes, the handle and the keys.
	envelopeAllowance = 8 << 10
	// maxDocument is the most a document on stdin may be: an issued seat and the answer around it.
	maxDocument = seat.MaxBundle + seat.MaxSecret + envelopeAllowance
	// answerVersion is the v of the answer teamchat-admin prints (its wire major); the contract
	// test in package admin holds the two together.
	answerVersion = 1
	// contentAfterValue is proto.Parse's rule for a document with bytes after its value.
	contentAfterValue = "content after the value"
	// jsonSpace is the white space a JSON document may be padded with.
	jsonSpace = " \t\r\n"
)

// Read is the issued seat on r: strict JSON within maxDocument bytes, either the issued
// document itself (a seat and one credential) or the answer of teamchat-admin member add,
// return or reissue around it (its bundle member, with only the members that answer has), read
// by seat.DecodeIssued. A refusal is a *FailureError of 78 bundle that quotes nothing of the
// document; a read error is returned as it is.
func Read(r io.Reader) (seat.Issued, error) {
	data, err := io.ReadAll(io.LimitReader(r, maxDocument+1))
	if err != nil {
		return seat.Issued{}, fmt.Errorf("placeissued: read stdin: %w", err)
	}
	if len(data) > maxDocument {
		return seat.Issued{}, refused(ErrTooBig, msgTooBig, "")
	}
	if len(bytes.Trim(data, jsonSpace)) == 0 {
		return seat.Issued{}, refused(ErrEmpty, msgEmpty, "")
	}
	doc, err := proto.Parse(data)
	if err != nil {
		return seat.Issued{}, notStrict(err)
	}
	issued, err := issuedPart(doc)
	if err != nil {
		return seat.Issued{}, err
	}
	is, err := seat.DecodeIssued(issued)
	if err != nil {
		return seat.Issued{}, refused(err, msgInvalid, err.Error())
	}
	return is, nil
}

// notStrict is the FailureError of a document proto.Parse refused: trailing bytes after a first
// value, or anything else that strict JSON forbids. The cause is the parser's own text, which
// names a rule and an offset and never the input.
func notStrict(err error) error {
	if syntax, ok := errors.AsType[*proto.SyntaxError](err); ok && syntax.Rule == contentAfterValue {
		return refused(fmt.Errorf("%w: %w", ErrTrailing, err), msgTrailing, err.Error())
	}
	return refused(fmt.Errorf("%w: %w", ErrNotJSON, err), msgNotJSON, err.Error())
}

// issuedPart is the compact text of the issued document inside doc: the bundle of an answer (a
// document with a bundle member), or doc itself when it is a seat (a document with a seat
// member).
func issuedPart(doc proto.Value) ([]byte, error) {
	if doc.Kind() != proto.KindObject {
		return nil, refused(ErrNotIssued, msgNotIssued, "")
	}
	if bundle, ok := doc.Get("bundle"); ok {
		if !isAnswer(doc) {
			return nil, refused(ErrEnvelope, msgEnvelope, "")
		}
		return bundle.Compact(), nil
	}
	if _, ok := doc.Get("seat"); ok {
		return doc.Compact(), nil
	}
	return nil, refused(ErrNotIssued, msgNotIssued, "")
}

// isAnswer is whether doc holds exactly the members of the answer teamchat-admin prints for a
// member verb that issues a credential: v, changes and the bundle always, a handle and notes when
// it has them, each of its type.
func isAnswer(doc proto.Value) bool {
	for _, key := range []string{"v", "changed", "bundle"} {
		if _, found := doc.Get(key); !found {
			return false
		}
	}
	for _, m := range doc.Members() {
		if !answerMember(m) {
			return false
		}
	}
	return true
}

func answerMember(m proto.Member) bool {
	switch m.Key {
	case "v":
		return m.Val.Kind() == proto.KindInt && m.Val.Int() == answerVersion
	case "changed", "notes":
		return textLines(m.Val)
	case "handle":
		return m.Val.Kind() == proto.KindString
	case "bundle":
		return m.Val.Kind() == proto.KindObject
	}
	return false
}

// textLines is whether v is an array of strings.
func textLines(v proto.Value) bool {
	if v.Kind() != proto.KindArray {
		return false
	}
	for _, item := range v.Items() {
		if item.Kind() != proto.KindString {
			return false
		}
	}
	return true
}
