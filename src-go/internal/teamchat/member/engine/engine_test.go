package engine

import "testing"

func TestNewRefusesAnIncompleteConfig(t *testing.T) {
	if _, err := New(Config{}); err == nil {
		t.Fatal("New of an empty config succeeded")
	}
}
