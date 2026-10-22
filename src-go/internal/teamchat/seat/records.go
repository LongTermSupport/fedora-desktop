package seat

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/matrix"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/proto"
)

// ErrInvalid is a record, bundle or secret that fails its rules (SR1-SR3).
var ErrInvalid = errors.New("seat: invalid")

const (
	recordVersion = 1
	// maxRecord bounds every record the store reads (SW3).
	maxRecord = 16 << 10
	// maxSecret bounds a credential, as S19 bounds a token.
	maxSecret = 4096
	// checkToken stands in for the token a member client needs when only its URL rule is asked.
	checkToken = "unused"
	keyClaim   = "claim"
)

// Bundle is a seat's seat.json in the pool's form (C 3.7.2): no claim and no release time.
type Bundle struct {
	V              int          `json:"v"`
	Team           paths.TeamID `json:"team"`
	ServerName     string       `json:"server_name"`
	UserID         string       `json:"user_id"`
	AdminID        string       `json:"admin_id"`
	MainRoom       string       `json:"main_room"`
	BaseURL        string       `json:"base_url"`
	PlainHTTPHosts []string     `json:"plain_http_hosts"`
	DeviceID       string       `json:"device_id"`
	IssuedMS       int64        `json:"issued_ms"`
	Issuer         paths.Digest `json:"issuer"`
}

var bundleKeys = []string{
	"v", "team", "server_name", "user_id", "admin_id", "main_room", "base_url", "plain_http_hosts",
	"device_id", "issued_ms", "issuer",
}

// HeldBundle is seat.json as a take writes it, into held/ and the session's seat directory:
// the pool's with the take's claim and the seat's last release time, nil for a seat never
// released (C 3.7.2).
type HeldBundle struct {
	Bundle

	Claim      paths.ClaimID `json:"claim"`
	ReleasedMS *int64        `json:"released_ms"`
}

// Handle is the seat's handle, the localpart of user_id; a decoded bundle always has one.
func (b Bundle) Handle() proto.Handle {
	h, ok := proto.AgentHandle(b.UserID, b.ServerName)
	if !ok {
		panic("seat: Handle of a bundle that was not decoded")
	}
	return h
}

// Encode is the pool's seat.json.
func (b Bundle) Encode() ([]byte, error) {
	if err := b.check(); err != nil {
		return nil, err
	}
	return json.Marshal(b)
}

// Encode is the held seat.json.
func (h HeldBundle) Encode() ([]byte, error) {
	if err := h.check(); err != nil {
		return nil, err
	}
	return json.Marshal(h)
}

// DecodeBundle reads a pool seat.json (SR1).
func DecodeBundle(data []byte) (Bundle, error) {
	var b Bundle
	if err := decodeStrict(data, bundleKeys, &b); err != nil {
		return Bundle{}, err
	}
	return b, b.check()
}

// DecodeHeldBundle reads a held seat.json (SR1).
func DecodeHeldBundle(data []byte) (HeldBundle, error) {
	var h HeldBundle
	if err := decodeStrict(data, append(slices.Clone(bundleKeys), keyClaim, "released_ms"), &h); err != nil {
		return HeldBundle{}, err
	}
	return h, h.check()
}

func (h HeldBundle) check() error {
	if err := h.Bundle.check(); err != nil {
		return err
	}
	if h.Claim.String() == "" {
		return invalid("seat.json: no claim")
	}
	if h.ReleasedMS != nil && *h.ReleasedMS < 0 {
		return invalid("seat.json: released_ms is negative")
	}
	return nil
}

func (b Bundle) check() error {
	if b.V != recordVersion {
		return invalid("seat.json: v is not 1")
	}
	if b.Team.String() == "" || b.ServerName != paths.ServerName(b.Team) {
		return invalid("seat.json: server_name is not the team's")
	}
	if _, ok := proto.AgentHandle(b.UserID, b.ServerName); !ok {
		return invalid("seat.json: user_id is not an agent handle on the team's server")
	}
	if b.AdminID != proto.AdminID(b.ServerName) {
		return invalid("seat.json: admin_id is not the team's admin")
	}
	if !proto.ValidRoomID(b.MainRoom) {
		return invalid("seat.json: main_room is not a room ID")
	}
	c, err := matrix.NewMember(matrix.Config{BaseURL: b.BaseURL, PlainHTTPHosts: b.PlainHTTPHosts, Token: checkToken})
	if err != nil {
		return invalid("seat.json: base_url is not a member URL its plain_http_hosts allow")
	}
	c.Close()
	if b.DeviceID != paths.SeatDevice {
		return invalid("seat.json: device_id is not the seat's")
	}
	if b.IssuedMS < 0 {
		return invalid("seat.json: issued_ms is negative")
	}
	if b.Issuer.String() == "" {
		return invalid("seat.json: no issuer")
	}
	return nil
}

func invalid(detail string) error { return fmt.Errorf("%w %s", ErrInvalid, detail) }

// decodeStrict decodes one record: at most maxRecord bytes, proto's strict JSON (no repeated
// key, nothing after the value, integers only), an object holding every key of want and no
// other, then into out, where each typed field's own decoder checks it. Every key must be
// lowercase ASCII, as every record's keys are, since encoding/json matches a field name
// case-insensitively and would take `ISSUED_MS` as a second `issued_ms`.
func decodeStrict(data []byte, want []string, out any) error {
	if len(data) > maxRecord {
		return invalid("record: larger than its cap")
	}
	v, err := proto.Parse(data)
	if err != nil {
		return fmt.Errorf("%w record: %w", ErrInvalid, err)
	}
	if v.Kind() != proto.KindObject {
		return invalid("record: not an object")
	}
	for _, m := range v.Members() {
		if strings.IndexFunc(m.Key, func(r rune) bool { return (r < 'a' || r > 'z') && r != '_' }) >= 0 {
			return invalid("record: a key that is not lowercase ASCII")
		}
	}
	for _, key := range want {
		if _, ok := v.Get(key); !ok {
			return invalid("record: no " + key)
		}
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(out); err != nil {
		return fmt.Errorf("%w record: %w", ErrInvalid, err)
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return invalid("record: content after the value")
	}
	return nil
}

// HeldRecord is held/held.json, the holding record (C 3.7.1).
type HeldRecord struct {
	V       int               `json:"v"`
	Session paths.SessionName `json:"session"`
	Claim   paths.ClaimID     `json:"claim"`
	SinceMS int64             `json:"since_ms"`
}

// ReleasedRecord is released.json, the seat's last release (C 3.7.2).
type ReleasedRecord struct {
	V          int           `json:"v"`
	Claim      paths.ClaimID `json:"claim"`
	ReleasedMS int64         `json:"released_ms"`
}

// NoticeRecord is held/notice.json, the disconnected notice judged for one run (C 3.7.3
// release step 3): posted (or a goodbye found), or not possible for good, and why. A not-posted
// record with UntilMS is a transient failure still being retried until then, or, past it, one a
// release gave up (n2e-D7).
type NoticeRecord struct {
	V       int           `json:"v"`
	Claim   paths.ClaimID `json:"claim"`
	Run     paths.RunID   `json:"run"`
	Posted  bool          `json:"posted"`
	Why     string        `json:"why,omitempty"`
	UntilMS int64         `json:"until_ms,omitempty"`
}

func encodeRecord(v any) ([]byte, error) { return json.Marshal(v) }

func decodeHeldRecord(data []byte) (HeldRecord, error) {
	var r HeldRecord
	if err := decodeStrict(data, []string{"v", "session", keyClaim, "since_ms"}, &r); err != nil {
		return HeldRecord{}, err
	}
	if r.V != recordVersion || r.SinceMS < 0 {
		return HeldRecord{}, invalid("held.json: v is not 1 or since_ms is negative")
	}
	return r, nil
}

func decodeReleasedRecord(data []byte) (ReleasedRecord, error) {
	var r ReleasedRecord
	if err := decodeStrict(data, []string{"v", keyClaim, "released_ms"}, &r); err != nil {
		return ReleasedRecord{}, err
	}
	if r.V != recordVersion || r.ReleasedMS < 0 {
		return ReleasedRecord{}, invalid("released.json: v is not 1 or released_ms is negative")
	}
	return r, nil
}

func decodeNoticeRecord(data []byte) (NoticeRecord, error) {
	var r NoticeRecord
	if err := decodeStrict(data, []string{"v", keyClaim, "run", "posted"}, &r); err != nil {
		return NoticeRecord{}, err
	}
	if r.V != recordVersion || r.Posted == (r.Why != "") || !oneLine(r.Why) {
		return NoticeRecord{}, invalid("notice.json: v is not 1, or why is not one line given only when not posted")
	}
	if r.UntilMS < 0 || r.Posted && r.UntilMS != 0 {
		return NoticeRecord{}, invalid("notice.json: until_ms is negative, or on a posted notice")
	}
	return r, nil
}

// oneLine is text with no control character.
func oneLine(s string) bool {
	for _, r := range s {
		if r < 0x20 || r == 0x7f {
			return false
		}
	}
	return true
}

// Kind is a seat's credential: the key it is issued under and the pool file that keeps it.
type Kind string

// The credentials (C 3.6.3): a pool seat's password (DC16 (c')), a guest seat's token.
const (
	KindPassword Kind = "password"
	KindToken    Kind = "token"
)

// Secret is a credential: printable ASCII without a space, 1 to maxSecret bytes. Formatting it
// never shows it, and it is never marshalled; Reveal is for the one place that stores or sends
// it.
type Secret struct{ s string }

// ParseSecret checks a credential.
func ParseSecret(s string) (Secret, error) {
	if s == "" || len(s) > maxSecret {
		return Secret{}, invalid("credential: not 1 to 4096 bytes")
	}
	for i := range len(s) {
		if s[i] < 0x21 || s[i] > 0x7e {
			return Secret{}, invalid("credential: not printable ASCII without a space")
		}
	}
	return Secret{s}, nil
}

// Reveal is the credential itself.
func (s Secret) Reveal() string { return s.s }

// Format prints a placeholder, whatever the verb.
func (Secret) Format(f fmt.State, _ rune) {
	if _, err := io.WriteString(f, "[secret]"); err != nil {
		return
	}
}

// MarshalText refuses, so a credential cannot reach JSON or text by accident.
func (Secret) MarshalText() ([]byte, error) {
	return nil, errors.New("seat: a credential is not marshalled")
}

// Issued is the bundle member add, return and reissue answer (SR3): the pool's seat.json and
// one credential.
type Issued struct {
	Seat   Bundle
	Kind   Kind
	Secret Secret
}

// Encode is the issued bundle's one JSON document, the only encoder that writes a secret.
func (i Issued) Encode() ([]byte, error) {
	if i.Kind != KindPassword && i.Kind != KindToken {
		return nil, invalid("issued bundle: no credential kind")
	}
	if _, err := ParseSecret(i.Secret.s); err != nil {
		return nil, err
	}
	seat, err := i.Seat.Encode()
	if err != nil {
		return nil, err
	}
	return json.Marshal(map[string]any{"seat": json.RawMessage(seat), string(i.Kind): i.Secret.s})
}

// DecodeIssued reads an issued bundle strictly.
func DecodeIssued(data []byte) (Issued, error) {
	v, err := proto.Parse(data)
	if err != nil || v.Kind() != proto.KindObject || len(v.Members()) != 2 {
		return Issued{}, invalid("issued bundle: not an object of a seat and one credential")
	}
	seat, ok := v.Get("seat")
	if !ok {
		return Issued{}, invalid("issued bundle: no seat")
	}
	for _, kind := range []Kind{KindPassword, KindToken} {
		cred, has := v.Get(string(kind))
		if !has {
			continue
		}
		if cred.Kind() != proto.KindString {
			return Issued{}, invalid("issued bundle: the credential is not a string")
		}
		secret, err := ParseSecret(cred.Str())
		if err != nil {
			return Issued{}, err
		}
		b, err := DecodeBundle(seat.Compact())
		if err != nil {
			return Issued{}, err
		}
		return Issued{Seat: b, Kind: kind, Secret: secret}, nil
	}
	return Issued{}, invalid("issued bundle: no password or token")
}
