package proto

import (
	"errors"
	"strings"
	"testing"
)

func TestBuiltTextValidates(t *testing.T) {
	re := "$" + strings.Repeat("A", 43)
	content, err := BuildText([]string{testWorker, testAlice}, "hello", re)
	if err != nil {
		t.Fatal(err)
	}
	text, err := ValidateTextContent(content, testContext(), true)
	if err != nil || text.Text != "hello" || len(text.To) != 2 || text.To[0] != testAlice {
		t.Fatalf("%+v %v", text, err)
	}
	if got := RenderText([]string{testWorker, testAlice}, "x"); got != "[agent-bus] text -> "+testAlice+" "+testWorker+"\nx" {
		t.Fatalf("%q", got)
	}
}

func TestBuildTextRefusesABadRe(t *testing.T) {
	if _, err := BuildText([]string{testWorker}, "x", "$short"); !errors.Is(err, ErrNotEventID) {
		t.Fatalf("%v", err)
	}
}

func TestTextSecretOnlyOnSend(t *testing.T) {
	content, err := BuildText([]string{testWorker}, "$ANSIBLE_VAULT;1.1", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ValidateTextContent(content, testContext(), true); err == nil || err.Error() != "secret" {
		t.Fatalf("%v", err)
	}
	if _, err := ValidateTextContent(content, testContext(), false); err != nil {
		t.Fatal(err)
	}
}
