package conversation

import "testing"

// A test writes the record where the locator reads it: clean.
func TestRead(t *testing.T) {
	if read("/c", "s") != "/c/.claude/ccy/pingbus/sessions/s/"+File+"" {
		t.Fatal("path")
	}
	_ = "conversation.json"
}
