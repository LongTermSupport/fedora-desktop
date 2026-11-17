package carry

import (
	"bytes"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
)

// SC3, SC7: the seal record at each step encodes to the spec's shape and decodes strictly back.
func TestSealRecordRoundTrip(t *testing.T) {
	team := sampleTeam(t)
	seal := sampleSeal(t)
	for name, c := range map[string]struct {
		rec  SealRecord
		want string
	}{
		"sealing": {
			SealRecord{Team: team, Step: StepSealing, StartedMS: 1700000000000, Tail: seal.Tail},
			`{"v":1,"team":"ops","step":"sealing","tail":[{"room":"main","event_id":"$abc:ops.teamchat.internal","ts_ms":1699999999000}],"started_ms":1700000000000}`,
		},
		"sealed": {
			SealRecord{Team: team, Step: StepSealed, Seal: seal},
			`{"v":1,"team":"ops","step":"sealed","sealed_ms":1700000000123,"server_name":"ops.teamchat.internal","port":8448,"tuwunel":"1.4.0","software":"` +
				strings.Repeat("ab", 32) + `","roster":{"seq":12,"rev":7,"restore_epoch":1},"epoch_floor":{"epoch":1,"min_backup":3},"tail":[{"room":"main","event_id":"$abc:ops.teamchat.internal","ts_ms":1699999999000}]}`,
		},
	} {
		raw := EncodeSealRecord(c.rec)
		if string(raw) != c.want {
			t.Errorf("%s:\n got %s\nwant %s", name, raw, c.want)
		}
		got, err := DecodeSealRecord(raw, team)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if !bytes.Equal(EncodeSealRecord(got), raw) {
			t.Errorf("%s: the decoded record encodes differently", name)
		}
	}
}

// SD2: the drill record, at step stopping with the tail.
func TestDrillRecordRoundTrip(t *testing.T) {
	team := sampleTeam(t)
	rec := DrillRecord{Team: team, Step: StepStopping, StartedMS: 1700000000000, Tail: sampleSeal(t).Tail}
	raw := EncodeDrillRecord(rec)
	want := `{"v":1,"team":"ops","step":"stopping","tail":[{"room":"main","event_id":"$abc:ops.teamchat.internal","ts_ms":1699999999000}],"started_ms":1700000000000}`
	if string(raw) != want {
		t.Errorf("got %s\nwant %s", raw, want)
	}
	got, err := DecodeDrillRecord(raw, team)
	if err != nil || !bytes.Equal(EncodeDrillRecord(got), raw) {
		t.Fatalf("%v", err)
	}
}

// SI9 step 4: carried.json.
func TestCarriedRecordRoundTrip(t *testing.T) {
	team := sampleTeam(t)
	rec := CarriedRecord{
		Team: team, Kind: KindSealed, SHA256: strings.Repeat("c", 64), SealedMS: 1700000000123, ImportedMS: 1700000100000,
		Roster: RosterFacts{Seq: 12, Rev: 7, RestoreEpoch: 1}, Tail: sampleSeal(t).Tail,
	}
	raw := EncodeCarriedRecord(rec)
	want := `{"v":1,"team":"ops","kind":"sealed","sha256":"` + strings.Repeat("c", 64) + `","sealed_ms":1700000123,"imported_ms":1700000100000,"roster":{"seq":12,"rev":7,"restore_epoch":1},` +
		`"tail":[{"room":"main","event_id":"$abc:ops.teamchat.internal","ts_ms":1699999999000}]}`
	want = strings.Replace(want, `"sealed_ms":1700000123`, `"sealed_ms":1700000000123`, 1)
	if string(raw) != want {
		t.Errorf("got %s\nwant %s", raw, want)
	}
	got, err := DecodeCarriedRecord(raw, team)
	if err != nil || !bytes.Equal(EncodeCarriedRecord(got), raw) {
		t.Fatalf("%v", err)
	}
}

// SI8, SI9: the import record at each step, its placed roots a list of the team's roots.
func TestImportRecordRoundTrip(t *testing.T) {
	team := sampleTeam(t)
	for _, rec := range []ImportRecord{
		{Team: team, Kind: KindDrill, SHA256: strings.Repeat("d", 64), Step: ImportStaging},
		{Team: team, Kind: KindSealed, SHA256: strings.Repeat("d", 64), Step: ImportStaged},
		{Team: team, Kind: KindSealed, SHA256: strings.Repeat("d", 64), Step: ImportPlacing, Placed: []string{paths.HomeserverPrivateState(team), paths.CtlState(team)}},
	} {
		raw := EncodeImportRecord(rec)
		if rec.Step == ImportStaging {
			want := `{"v":1,"team":"ops","kind":"drill","sha256":"` + strings.Repeat("d", 64) + `","step":"staging","placed":[]}`
			if string(raw) != want {
				t.Errorf("got %s\nwant %s", raw, want)
			}
		}
		got, err := DecodeImportRecord(raw, team)
		if err != nil || !bytes.Equal(EncodeImportRecord(got), raw) {
			t.Fatalf("%s: %v", rec.Step, err)
		}
	}
}

// Every record decodes strictly: another team, an unknown key, a missing key, a bad step, a
// wrong version, a placed path that is not one of the team's placed roots, or not JSON, is
// refused, and the refusal never quotes the record.
func TestRecordsDecodeStrictly(t *testing.T) {
	team := sampleTeam(t)
	sealing := string(EncodeSealRecord(SealRecord{Team: team, Step: StepSealing, StartedMS: 1, Tail: sampleSeal(t).Tail}))
	sealed := string(EncodeSealRecord(SealRecord{Team: team, Step: StepSealed, Seal: sampleSeal(t)}))
	drill := string(EncodeDrillRecord(DrillRecord{Team: team, Step: StepStopping, StartedMS: 1, Tail: sampleSeal(t).Tail}))
	carried := string(EncodeCarriedRecord(CarriedRecord{Team: team, Kind: KindSealed, SHA256: strings.Repeat("c", 64), SealedMS: 1, ImportedMS: 2, Tail: sampleSeal(t).Tail}))
	imp := string(EncodeImportRecord(ImportRecord{Team: team, Kind: KindSealed, SHA256: strings.Repeat("d", 64), Step: ImportPlacing, Placed: []string{paths.AdminState(team)}}))
	decoders := map[string]func(string) error{
		"seal":    func(s string) error { _, err := DecodeSealRecord([]byte(s), team); return err },
		"drill":   func(s string) error { _, err := DecodeDrillRecord([]byte(s), team); return err },
		"carried": func(s string) error { _, err := DecodeCarriedRecord([]byte(s), team); return err },
		"import":  func(s string) error { _, err := DecodeImportRecord([]byte(s), team); return err },
	}
	for name, c := range map[string]struct{ decoder, raw string }{
		"seal of another team":       {"seal", strings.Replace(sealing, `"ops"`, `"dev"`, 1)},
		"seal with an unknown key":   {"seal", strings.Replace(sealing, `"v":1,`, `"v":1,"x":1,`, 1)},
		"seal at no known step":      {"seal", strings.Replace(sealing, `"sealing"`, `"done"`, 1)},
		"sealing with a seal":        {"seal", strings.Replace(sealed, `"step":"sealed"`, `"step":"sealing"`, 1)},
		"sealed without a seal":      {"seal", strings.Replace(sealing, `"step":"sealing"`, `"step":"sealed"`, 1)},
		"seal of version 2":          {"seal", strings.Replace(sealed, `"v":1`, `"v":2`, 1)},
		"seal not JSON":              {"seal", sealed[:20]},
		"drill without its tail":     {"drill", strings.Replace(drill, `"tail":[{"room":"main","event_id":"$abc:ops.teamchat.internal","ts_ms":1699999999000}],`, ``, 1)},
		"drill at another step":      {"drill", strings.Replace(drill, `"stopping"`, `"sealing"`, 1)},
		"carried of no kind":         {"carried", strings.Replace(carried, `"sealed"`, `"other"`, 1)},
		"carried short digest":       {"carried", strings.Replace(carried, strings.Repeat("c", 64), "cc", 1)},
		"carried without its tail":   {"carried", strings.Replace(carried, `,"tail":[{"room":"main","event_id":"$abc:ops.teamchat.internal","ts_ms":1699999999000}]`, ``, 1)},
		"carried with an empty tail": {"carried", strings.Replace(carried, `"tail":[{"room":"main","event_id":"$abc:ops.teamchat.internal","ts_ms":1699999999000}]`, `"tail":[]`, 1)},
		"import placing elsewhere":   {"import", strings.Replace(imp, paths.AdminState(team), "/etc/passwd", 1)},
		"import placing twice":       {"import", strings.Replace(imp, `"placed":["`+paths.AdminState(team)+`"]`, `"placed":["`+paths.AdminState(team)+`","`+paths.AdminState(team)+`"]`, 1)},
		"import of another team":     {"import", strings.Replace(imp, `"team":"ops"`, `"team":"dev"`, 1)},
	} {
		err := decoders[c.decoder](c.raw)
		if err == nil {
			t.Errorf("%s: decoded", name)
			continue
		}
		if strings.Contains(err.Error(), "/etc/passwd") || strings.Contains(err.Error(), "abc:") {
			t.Errorf("%s: the refusal quotes the record: %v", name, err)
		}
	}
}
