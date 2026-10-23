package rclonetail

import "testing"

// E9: the counters of vfs/stats and core/stats; missing or null ones are 0, floats truncated, a
// missing or null eta is no estimate.
func TestParseActivity(t *testing.T) {
	got, err := parseQueue([]byte(`{"diskCache":{"uploadsInProgress":2,"uploadsQueued":7.9,"erroredFiles":1,"bytesUsed":5}}`))
	if err != nil || got != (queue{inProgress: 2, queued: 7, errored: 1}) {
		t.Fatalf("vfs: %+v %v", got, err)
	}
	for _, in := range []string{`{}`, `{"diskCache":null}`, `{"diskCache":{"uploadsQueued":null}}`} {
		if q, qerr := parseQueue([]byte(in)); qerr != nil || q != (queue{}) {
			t.Errorf("vfs %s: %+v %v", in, q, qerr)
		}
	}
	tr, err := parseTransfers([]byte(`{"bytes":1536,"totalBytes":4096.5,"transfers":3,"totalTransfers":9,"eta":125.7}`))
	if err != nil || tr != (transfers{bytes: 1536, totalBytes: 4096, done: 3, total: 9, eta: 125, hasETA: true}) {
		t.Fatalf("core: %+v %v", tr, err)
	}
	for _, in := range []string{`{}`, `{"eta":null,"bytes":null}`} {
		if tr, err := parseTransfers([]byte(in)); err != nil || tr != (transfers{}) {
			t.Errorf("core %s: %+v %v", in, tr, err)
		}
	}
}

// E8: anything else is "parse failed".
func TestParseActivityRefusals(t *testing.T) {
	for _, in := range []string{
		``, `[]`, `"x"`, `{"diskCache":[]}`, `{"diskCache":{"uploadsQueued":"many"}}`, `{} {}`,
		`{"diskCache":{"erroredFiles":1e40}}`,
	} {
		if got, err := parseQueue([]byte(in)); err == nil {
			t.Errorf("vfs %q: parsed as %+v", in, got)
		}
	}
	for _, in := range []string{``, `[]`, `{"bytes":"x"}`, `{"eta":"soon"}`, `{"eta":1e40}`, `{} {}`} {
		if got, err := parseTransfers([]byte(in)); err == nil {
			t.Errorf("core %q: parsed as %+v", in, got)
		}
	}
}

// E12: the ETA as minutes and seconds; no estimate or 0 is a dash.
func TestETA(t *testing.T) {
	cases := []struct {
		s    int64
		has  bool
		want string
	}{
		{0, false, "—"},
		{0, true, "—"},
		{5, true, "0m05s"},
		{60, true, "1m00s"},
		{3665, true, "61m05s"},
		{125, false, "—"},
	}
	for _, tc := range cases {
		if got := eta(tc.s, tc.has); got != tc.want {
			t.Errorf("eta(%d, %v) = %q, want %q", tc.s, tc.has, got, tc.want)
		}
	}
}
