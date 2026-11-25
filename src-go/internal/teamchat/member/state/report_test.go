package state

import (
	"errors"
	"strings"
	"testing"
	"unicode/utf8"
)

const reportApplied = `{"v":1,"run":"` + sampleRun + `","started_ms":1760000000000,"seat":"alpha/2","handle":"repo.2+site.podman",` +
	`"link":"running","why":null,"last_sync_ok_ms":1760000000000,"down":null,"pending":0,"delivery":"socket",` +
	`"desired":{"generation":3,"state":"applied","code":null},"store":"ok","needs_relaunch":false}`

func TestDecodeReport(t *testing.T) {
	refused := edit(t, reportApplied, `"state":"applied","code":null`, `"state":"refused","code":"missing"`)
	accepts(t, DecodeReport, reportApplied, refused,
		edit(t, reportApplied, `"last_sync_ok_ms":1760000000000`, `"last_sync_ok_ms":null`),
		edit(t, edit(t, reportApplied, `"alpha/2"`, `"alpha/lead"`), `repo.2+`, `repo.lead+`),
		edit(t, edit(t, reportApplied, `"store":"ok"`, `"store":"refused"`), `"delivery":"socket"`, `"delivery":"none"`),
		edit(t, reportApplied, `"why":null`, `"why":"launch-failed"`),
		edit(t, reportApplied, `"link":"running","why":null`, `"link":"failed","why":"crash-loop"`),
		edit(t, reportApplied, `"link":"running","why":null`, `"link":"failed","why":"config"`),
		// R60: a team the link has down names its cause; one never answered has no sync time.
		edit(t, edit(t, reportApplied, `"down":null`, `"down":"the homeserver did not answer (dial tcp: i/o timeout)"`),
			`"last_sync_ok_ms":1760000000000`, `"last_sync_ok_ms":null`),
		edit(t, reportApplied, `"down":null`, `"down":"`+strings.Repeat("d", MaxDown-2)+`"`))
	for _, code := range []string{CodeJSON, CodeSchema, CodeVersion, CodeSeat} {
		accepts(t, DecodeReport, edit(t, refused, `"missing"`, `"`+code+`"`))
	}
	r, err := DecodeReport([]byte(reportApplied))
	if err != nil || r.Seat.Team.String() != "alpha" || r.Seat.Seat != "2" {
		t.Fatalf("DecodeReport = %+v, %v", r, err)
	}
	strictRefusals(t, reportFile, reportApplied, "pending", MaxReport, DecodeReport)
	refusesAll(t, reportFile, reportApplied, DecodeReport, []refusal{
		{`"started_ms":1760000000000`, `"started_ms":-1`, CodeSchema},
		{`"alpha/2"`, `"alpha"`, CodeSchema},
		{`"alpha/2"`, `"SECRET/2"`, CodeSchema},
		{`"alpha/2"`, `"admin/2"`, CodeSchema},
		{`"alpha/2"`, `"alpha/SECRET"`, CodeSchema},
		{`"alpha/2"`, `"alpha/2/3"`, CodeSchema},
		{`"repo.2+site.podman"`, `"SECRET"`, CodeSchema},
		{`"repo.2+site.podman"`, `"repo.3+site.podman"`, CodeSchema},
		{`"running"`, `"SECRET"`, CodeSchema},
		{`"last_sync_ok_ms":1760000000000`, `"last_sync_ok_ms":-424242`, CodeSchema},
		{`"pending":0`, `"pending":-1`, CodeSchema},
		{`"pending":0`, `"pending":2049`, CodeSchema},
		{`"socket"`, `"SECRET"`, CodeSchema},
		{`"generation":3`, `"generation":-1`, CodeSchema},
		{`"state":"applied"`, `"state":"SECRET"`, CodeSchema},
		{`"code":null`, `"code":"json"`, CodeSchema},
		{`"store":"ok"`, `"store":"SECRET"`, CodeSchema},
		{`"needs_relaunch":false`, `"needs_relaunch":null`, CodeSchema},
		{`"desired":{"generation":3,"state":"applied","code":null}`, `"desired":null`, CodeSchema},
		{`"down":null`, `"down":""`, CodeSchema},
		{`"down":null`, `"down":"` + strings.Repeat("d", MaxDown-1) + `"`, CodeSchema},
		// The bound is the written form's: one escaped character is six bytes there.
		{`"down":null`, `"down":"` + strings.Repeat("<", (MaxDown+5)/6) + `"`, CodeSchema},
		{`"down":null,`, ``, CodeSchema},
	})
	refusesAll(t, reportFile, refused, DecodeReport, []refusal{
		{`"code":"missing"`, `"code":null`, CodeSchema},
		{`"code":"missing"`, `"code":"SECRET"`, CodeSchema},
	})
}

// TestReportCap holds the largest report.json within its cap.
func TestReportCap(t *testing.T) {
	big := "9007199254740991"
	doc := edit(t, reportApplied, `"alpha/2"`, `"`+longTeam(0)+`/`+strings.Repeat("a", 12)+`"`)
	doc = edit(t, doc, `"repo.2+site.podman"`, `"`+strings.Repeat("r", 48)+"."+strings.Repeat("a", 12)+"+"+strings.Repeat("s", 63)+`.podman"`)
	doc = edit(t, doc, `"link":"running","why":null`, `"link":"restarting","why":"launch-failed"`)
	doc = edit(t, doc, `"generation":3`, `"generation":`+big)
	doc = edit(t, doc, `"pending":0`, `"pending":2048`)
	doc = edit(t, doc, `"state":"applied","code":null`, `"state":"refused","code":"version"`)
	doc = edit(t, doc, `"down":null`, `"down":"`+strings.Repeat("d", MaxDown-2)+`"`)
	if len(doc) > MaxReport/2 {
		t.Fatalf("the largest report.json is %d bytes", len(doc))
	}
	accepts(t, DecodeReport, doc)
}

// R60: a down cause is clipped to what report.json takes, by its written form, never inside a
// character; a short one is kept whole, and an empty one is the generic cause.
func TestClipDownKeepsTheWrittenFormWithinItsBound(t *testing.T) {
	if got := ClipDown("dial tcp: i/o timeout"); got != "dial tcp: i/o timeout" {
		t.Errorf("a short cause = %q", got)
	}
	if got := ClipDown(""); got != DownNoAnswer {
		t.Errorf("an empty cause = %q", got)
	}
	for _, long := range []string{strings.Repeat("a", 2*MaxDown), strings.Repeat("<", MaxDown), strings.Repeat("é", MaxDown)} {
		got := ClipDown(long)
		n, err := writtenSize(got)
		check(t, err)
		if n > MaxDown || !utf8.ValidString(got) || !strings.HasPrefix(long, got) || len(got) == 0 {
			t.Errorf("%.8q... clipped to %d written bytes: %.8q", long, n, got)
		}
	}
}

func TestSeatRef(t *testing.T) {
	ref := SeatRef{Team: teamID(t), Seat: "lead"}
	text, err := ref.MarshalText()
	check(t, err)
	var back SeatRef
	if back.UnmarshalText(text) != nil || back != ref || string(text) != "alpha/lead" {
		t.Fatalf("SeatRef round trip = %q, %+v", text, back)
	}
	for _, bad := range []SeatRef{{Team: teamID(t)}, {Team: teamID(t), Seat: "0"}, {Seat: "2"}} {
		if _, err := bad.MarshalText(); !errors.Is(err, errSeatRef) {
			t.Errorf("%+v encoded: %v", bad, err)
		}
	}
}

func TestReportRoundTrip(t *testing.T) {
	r, err := DecodeReport([]byte(reportApplied))
	check(t, err)
	roundTrips(t, r, MaxReport, DecodeReport)
}
