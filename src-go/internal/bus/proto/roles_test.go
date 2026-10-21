package proto

import "testing"

func TestVerbOrderIsTheSpecs(t *testing.T) {
	want := []string{"fetch", "sync", "review", "run-qa", "halt", "ack", "nack", "done", "blocked"}
	if len(VerbOrder) != len(want) || len(Verbs) != len(want) {
		t.Fatalf("%v", VerbOrder)
	}
	for i, v := range want {
		if VerbOrder[i] != v {
			t.Fatalf("%v", VerbOrder)
		}
	}
	if err := CheckRole("nope", "@a:s", &Context{}); err == nil {
		t.Fatal("unknown verb")
	}
}
