package seat

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
)

func encode(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// reshape is doc with key set to value (JSON text), or removed when value is empty.
func reshape(t *testing.T, doc, key, value string) []byte {
	t.Helper()
	var m map[string]json.RawMessage
	if err := json.Unmarshal([]byte(doc), &m); err != nil {
		t.Fatal(err)
	}
	if value == "" {
		delete(m, key)
	} else {
		m[key] = json.RawMessage(value)
	}
	return []byte(encode(t, m))
}

// SR1: the pool form has the 3.7.2 fields and no claim or release time; it round-trips.
func TestPoolBundleRoundTrip(t *testing.T) {
	b := testBundle(t)
	data, err := b.Encode()
	if err != nil {
		t.Fatal(err)
	}
	got := decodeJSON(t, string(data))
	for _, key := range []string{
		"v", "team", "server_name", "user_id", "admin_id", "main_room", "base_url", "plain_http_hosts",
		"device_id", "issued_ms", "issuer",
	} {
		if _, ok := got[key]; !ok {
			t.Errorf("pool seat.json has no %s: %s", key, data)
		}
	}
	if len(got) != 11 {
		t.Errorf("pool seat.json has %d keys, want 11: %s", len(got), data)
	}
	back, err := DecodeBundle(data)
	if err != nil {
		t.Fatalf("DecodeBundle: %v", err)
	}
	if encode(t, back) != encode(t, b) {
		t.Errorf("round trip: %s, want %s", encode(t, back), encode(t, b))
	}
	if h := back.Handle(); h.String() != testHandle {
		t.Errorf("handle %q", h)
	}
}

// SR1: the held form adds the claim and the release time, null for a seat never released.
func TestHeldBundleCarriesClaimAndReleasedMS(t *testing.T) {
	claim, err := paths.ParseClaimID("00112233aabbccdd")
	if err != nil {
		t.Fatal(err)
	}
	for _, released := range []*int64{nil, new(int64(42))} {
		h := HeldBundle{Bundle: testBundle(t), Claim: claim, ReleasedMS: released}
		data, encErr := h.Encode()
		if encErr != nil {
			t.Fatal(encErr)
		}
		got := decodeJSON(t, string(data))
		if got["claim"] != claim.String() || len(got) != 13 {
			t.Errorf("held seat.json %s", data)
		}
		if raw, ok := got["released_ms"]; !ok || (released == nil) != (raw == nil) {
			t.Errorf("released_ms in %s", data)
		}
		back, decErr := DecodeHeldBundle(data)
		if decErr != nil {
			t.Fatalf("DecodeHeldBundle: %v", decErr)
		}
		if encode(t, back) != encode(t, h) {
			t.Errorf("round trip %s", encode(t, back))
		}
		if _, poolErr := DecodeBundle(data); poolErr == nil {
			t.Errorf("a held seat.json decoded as a pool record")
		}
	}
	pool, err := testBundle(t).Encode()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := DecodeHeldBundle(pool); err == nil {
		t.Errorf("a pool seat.json decoded as a held one")
	}
	if _, err := DecodeHeldBundle(reshape(t, string(pool), "claim", `"00112233aabbccdd"`)); err == nil {
		t.Errorf("a held seat.json without released_ms decoded")
	}
}

// SR1: every field is checked, and the decode is strict.
func TestBundleRefusals(t *testing.T) {
	good, err := testBundle(t).Encode()
	if err != nil {
		t.Fatal(err)
	}
	doc := string(good)
	server := paths.ServerName(mustTeam(t))
	cases := map[string][]byte{
		"v 2":                   reshape(t, doc, "v", `2`),
		"no team":               reshape(t, doc, "team", ""),
		"team invalid":          reshape(t, doc, "team", `"Ops"`),
		"server of another":     reshape(t, doc, "server_name", `"dev.teamchat.internal"`),
		"user not a handle":     reshape(t, doc, "user_id", `"@alice:`+server+`"`),
		"user on another":       reshape(t, doc, "user_id", `"@`+testHandle+`:example.org"`),
		"admin not the admin":   reshape(t, doc, "admin_id", `"@root:`+server+`"`),
		"room not a room":       reshape(t, doc, "main_room", `"#main:x"`),
		"http to a name":        reshape(t, doc, "base_url", `"http://example.org:8448"`),
		"http not listed":       reshape(t, doc, "plain_http_hosts", `[]`),
		"device not the seat's": reshape(t, doc, "device_id", `"OTHER"`),
		"issued negative":       reshape(t, doc, "issued_ms", `-1`),
		"issuer not a digest":   reshape(t, doc, "issuer", `"current"`),
		"unknown key":           reshape(t, doc, "extra", `1`),
		"repeated key":          []byte(strings.Replace(doc, `{"v":1,`, `{"v":1,"v":1,`, 1)),
		"a key in capitals":     []byte(strings.Replace(doc, `{"v":1,`, `{"v":1,"ISSUED_MS":5,`, 1)),
		"a key case-folded":     []byte(strings.Replace(doc, `{"v":1,`, `{"v":1,"Issued_Ms":5,`, 1)),
		"a key Unicode-folded":  []byte(strings.Replace(doc, `{"v":1,`, `{"v":1,"iſsued_ms":5,`, 1)),
		"trailing":              append(append([]byte(nil), good...), " {}"...),
		"not an object":         []byte(`[]`),
		"too large":             reshape(t, doc, "base_url", `"`+strings.Repeat("a", maxRecord)+`"`),
		"a float":               reshape(t, doc, "issued_ms", `1.5`),
	}
	for name, data := range cases {
		if _, err := DecodeBundle(data); err == nil {
			t.Errorf("%s: decoded", name)
		}
	}
}

func mustClaim(t *testing.T, s string) paths.ClaimID {
	t.Helper()
	c, err := paths.ParseClaimID(s)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func mustRun(t *testing.T, s string) paths.RunID {
	t.Helper()
	r, err := paths.ParseRunID(s)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

// SR2: the held, released and notice records round-trip.
func TestRecordsRoundTrip(t *testing.T) {
	claim := mustClaim(t, "00112233aabbccdd")
	run := mustRun(t, "0123456789abcdef")
	held := HeldRecord{V: 1, Session: mustSession(t, "sess-1"), Claim: claim, SinceMS: 5}
	data, err := encodeRecord(held)
	if err != nil {
		t.Fatal(err)
	}
	if got, decErr := decodeHeldRecord(data); decErr != nil || got != held {
		t.Errorf("held round trip %+v, %v", got, decErr)
	}
	if string(data) != `{"v":1,"session":"sess-1","claim":"00112233aabbccdd","since_ms":5}` {
		t.Errorf("held.json %s", data)
	}
	rel := ReleasedRecord{V: 1, Claim: claim, ReleasedMS: 9}
	if data, err = encodeRecord(rel); err != nil {
		t.Fatal(err)
	}
	if got, decErr := decodeReleasedRecord(data); decErr != nil || got != rel {
		t.Errorf("released round trip %+v, %v", got, decErr)
	}
	for _, n := range []NoticeRecord{
		{V: 1, Claim: claim, Run: run, Posted: true},
		{V: 1, Claim: claim, Run: run, Why: "token refused"},
	} {
		if data, err = encodeRecord(n); err != nil {
			t.Fatal(err)
		}
		if got, decErr := decodeNoticeRecord(data); decErr != nil || got != n {
			t.Errorf("notice round trip %+v, %v (%s)", got, decErr, data)
		}
	}
}

// SR2: each record is strict, and no decoder takes another's document.
func TestRecordsRefuse(t *testing.T) {
	for name, doc := range map[string]string{
		"held v2":            `{"v":2,"session":"s","claim":"00112233aabbccdd","since_ms":5}`,
		"held no session":    `{"v":1,"claim":"00112233aabbccdd","since_ms":5}`,
		"held bad claim":     `{"v":1,"session":"s","claim":"x","since_ms":5}`,
		"held negative":      `{"v":1,"session":"s","claim":"00112233aabbccdd","since_ms":-5}`,
		"held extra":         `{"v":1,"session":"s","claim":"00112233aabbccdd","since_ms":5,"x":1}`,
		"held case-folded":   `{"v":1,"session":"s","claim":"00112233aabbccdd","since_ms":5,"Since_MS":6}`,
		"released no claim":  `{"v":1,"released_ms":9}`,
		"notice posted why":  `{"v":1,"claim":"00112233aabbccdd","run":"0123456789abcdef","posted":true,"why":"x"}`,
		"notice no why":      `{"v":1,"claim":"00112233aabbccdd","run":"0123456789abcdef","posted":false}`,
		"notice bad run":     `{"v":1,"claim":"00112233aabbccdd","run":"x","posted":true}`,
		"notice why newline": `{"v":1,"claim":"00112233aabbccdd","run":"0123456789abcdef","posted":false,"why":"a\nb"}`,
	} {
		_, err1 := decodeHeldRecord([]byte(doc))
		_, err2 := decodeReleasedRecord([]byte(doc))
		_, err3 := decodeNoticeRecord([]byte(doc))
		for i, err := range []error{err1, err2, err3} {
			if err == nil {
				t.Errorf("%s decoded by decoder %d", name, i)
			}
		}
	}
}

// SR3: the issued bundle carries one credential, by its kind's key.
func TestIssuedBundle(t *testing.T) {
	for _, kind := range []Kind{KindPassword, KindToken} {
		is := Issued{Seat: testBundle(t), Kind: kind, Secret: mustSecret(t, "s3cret")}
		data, err := is.Encode()
		if err != nil {
			t.Fatal(err)
		}
		got := decodeJSON(t, string(data))
		if got[string(kind)] != "s3cret" || len(got) != 2 {
			t.Errorf("%s: %s", kind, data)
		}
		back, err := DecodeIssued(data)
		if err != nil {
			t.Fatalf("%s: %v", kind, err)
		}
		if back.Kind != kind || back.Secret.Reveal() != "s3cret" || encode(t, back.Seat) != encode(t, is.Seat) {
			t.Errorf("%s round trip %+v", kind, back)
		}
	}
	seatJSON, err := testBundle(t).Encode()
	if err != nil {
		t.Fatal(err)
	}
	for name, doc := range map[string]string{
		"both":     `{"seat":` + string(seatJSON) + `,"password":"a","token":"b"}`,
		"neither":  `{"seat":` + string(seatJSON) + `}`,
		"no seat":  `{"password":"a"}`,
		"empty":    `{"seat":` + string(seatJSON) + `,"password":""}`,
		"newline":  `{"seat":` + string(seatJSON) + `,"password":"a\nb"}`,
		"space":    `{"seat":` + string(seatJSON) + `,"password":"a b"}`,
		"other":    `{"seat":` + string(seatJSON) + `,"secret":"a"}`,
		"bad seat": `{"seat":{},"password":"a"}`,
		"number":   `{"seat":` + string(seatJSON) + `,"password":1}`,
		"too long": `{"seat":` + string(seatJSON) + `,"password":"` + strings.Repeat("a", maxSecret+1) + `"}`,
	} {
		if _, err := DecodeIssued([]byte(doc)); err == nil {
			t.Errorf("%s: decoded", name)
		}
	}
}

// SR3: a secret is never shown by formatting, nor marshalled by accident.
func TestSecretIsNeverShown(t *testing.T) {
	s := mustSecret(t, "hunter2")
	for _, verb := range []string{"%v", "%s", "%+v", "%#v", "%q", "%x"} {
		if out := fmt.Sprintf(verb, s); strings.Contains(out, "hunter2") || strings.Contains(out, fmt.Sprintf("%x", "hunter2")) {
			t.Errorf("%s shows the secret: %s", verb, out)
		}
	}
	if out := fmt.Sprintf("%+v", Issued{Secret: s}); strings.Contains(out, "hunter2") {
		t.Errorf("an issued bundle shows its secret: %s", out)
	}
	if _, err := json.Marshal(s); err == nil {
		t.Errorf("a secret marshalled")
	}
	for _, bad := range []string{"", "a b", "a\nb", "é", strings.Repeat("a", maxSecret+1)} {
		if _, err := ParseSecret(bad); !errors.Is(err, ErrInvalid) {
			t.Errorf("ParseSecret(%q) = %v", bad, err)
		}
	}
}

// SR1, SR3: an encoder refuses what a decoder would refuse, so nothing invalid is written.
func TestEncodersRefuseInvalid(t *testing.T) {
	bad := testBundle(t)
	bad.DeviceID = "OTHER"
	if _, err := bad.Encode(); !errors.Is(err, ErrInvalid) {
		t.Errorf("an invalid pool seat.json encoded: %v", err)
	}
	negative := int64(-1)
	for name, h := range map[string]HeldBundle{
		"bad seat":          {Bundle: bad, Claim: mustClaim(t, "00112233aabbccdd")},
		"no claim":          {Bundle: testBundle(t)},
		"negative released": {Bundle: testBundle(t), Claim: mustClaim(t, "00112233aabbccdd"), ReleasedMS: &negative},
	} {
		if _, err := h.Encode(); !errors.Is(err, ErrInvalid) {
			t.Errorf("%s: encoded: %v", name, err)
		}
	}
	for name, is := range map[string]Issued{
		"no kind":   {Seat: testBundle(t), Secret: mustSecret(t, "a")},
		"no secret": {Seat: testBundle(t), Kind: KindPassword},
		"bad seat":  {Seat: bad, Kind: KindPassword, Secret: mustSecret(t, "a")},
	} {
		if _, err := is.Encode(); !errors.Is(err, ErrInvalid) {
			t.Errorf("%s: encoded: %v", name, err)
		}
	}
	if _, err := decodeReleasedRecord([]byte(`{"v":1,"claim":"00112233aabbccdd","released_ms":-1}`)); err == nil {
		t.Errorf("a negative released_ms decoded")
	}
}

// SR1: Handle is only for a decoded bundle; asking it of anything else is a programming error.
func TestHandleOfUndecodedBundlePanics(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Errorf("Handle of an empty bundle did not panic")
		}
	}()
	t.Errorf("Handle answered %v", Bundle{}.Handle())
}

// SPEC-n3 gap 3: a member reads exactly the two seat.json shapes seat writes, a guest's pool
// form and a session's held form, chosen by the claim key; nothing in between decodes.
func TestDecodeMemberBundle(t *testing.T) {
	pool, err := testBundle(t).Encode()
	if err != nil {
		t.Fatal(err)
	}
	guest, err := DecodeMemberBundle(pool)
	if err != nil || guest.Held != nil || encode(t, guest.Bundle) != encode(t, testBundle(t)) {
		t.Errorf("a guest's seat.json: %+v, %v", guest, err)
	}
	h := HeldBundle{Bundle: testBundle(t), Claim: mustClaim(t, "ffeeddccbbaa9988"), ReleasedMS: new(int64(7))}
	heldData, err := h.Encode()
	if err != nil {
		t.Fatal(err)
	}
	held, err := DecodeMemberBundle(heldData)
	if err != nil || held.Held == nil || encode(t, *held.Held) != encode(t, h) || encode(t, held.Bundle) != encode(t, h.Bundle) {
		t.Errorf("a session's seat.json: %+v, %v", held, err)
	}
	for name, data := range map[string][]byte{
		"released_ms without a claim":  reshape(t, string(pool), "released_ms", "1"),
		"a claim without released_ms":  reshape(t, string(pool), "claim", `"00112233aabbccdd"`),
		"a held form with another key": reshape(t, string(heldData), "extra", "1"),
		"a held form failing a check":  reshape(t, string(heldData), "device_id", `"OTHER"`),
		"not an object":                []byte(`[]`),
		"not JSON":                     []byte(`{`),
	} {
		if _, err := DecodeMemberBundle(data); !errors.Is(err, ErrInvalid) {
			t.Errorf("%s: %v", name, err)
		}
	}
}
