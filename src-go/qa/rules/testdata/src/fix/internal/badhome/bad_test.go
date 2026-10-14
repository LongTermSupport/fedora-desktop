package badhome

import (
	"strings"
	"testing"
)

func TestHand(t *testing.T) {
	_ = strings.Replace("/h/x", "/h", "~", 1) // want `strings.Replace with "~"`
}
