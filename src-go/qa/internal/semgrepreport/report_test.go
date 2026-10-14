package semgrepreport

import (
	"encoding/json"
	"testing"
)

func TestAResultDecodesFromSemgrepsJSON(t *testing.T) {
	const doc = `{"check_id": "rule-id", "path": "a/b.py", "start": {"line": 7}, "extra": {"message": "why"}}`
	var r Result
	if err := json.Unmarshal([]byte(doc), &r); err != nil {
		t.Fatal(err)
	}
	if r.CheckID != "rule-id" || r.Path != "a/b.py" || r.Start.Line != 7 || r.Extra.Message != "why" {
		t.Fatalf("%+v", r)
	}
}
