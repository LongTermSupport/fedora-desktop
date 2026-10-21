package rclonecache

import "testing"

// E13: the counters vfs/stats carries, missing or null ones as 0, floats truncated.
func TestParseStats(t *testing.T) {
	got, err := parseStats([]byte(`{
	  "diskCache": {"bytesUsed": 1536, "files": 3, "uploadsQueued": 2, "uploadsInProgress": 1,
	                "erroredFiles": 4, "outOfSpace": true},
	  "opt": {"CacheMaxSize": 10240.9, "CacheMaxAge": 3600000000000}
	}`))
	want := stats{used: 1536, files: 3, queued: 2, inProgress: 1, errored: 4, outOfSpace: true, max: 10240, maxAgeS: 3600}
	if err != nil || got != want {
		t.Fatalf("%+v %v", got, err)
	}
	got, err = parseStats([]byte(`{"diskCache": null, "opt": {"CacheMaxSize": null}}`))
	if err != nil || got != (stats{}) {
		t.Fatalf("nulls: %+v %v", got, err)
	}
	got, err = parseStats([]byte(`{}`))
	if err != nil || got != (stats{}) {
		t.Fatalf("empty: %+v %v", got, err)
	}
}

// E12: anything else is "parse failed".
func TestParseStatsRefusals(t *testing.T) {
	for _, in := range []string{
		``, `[]`, `"x"`, `{"diskCache": {"bytesUsed": "lots"}}`, `{"diskCache": {"outOfSpace": 1}}`,
		`{"opt": {"CacheMaxAge": 1e40}}`, `{"diskCache": []}`, `{} trailing`,
	} {
		if got, err := parseStats([]byte(in)); err == nil {
			t.Errorf("%q: parsed as %+v", in, got)
		}
	}
}
