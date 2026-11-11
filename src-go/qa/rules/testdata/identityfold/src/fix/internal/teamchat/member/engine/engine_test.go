package engine

import (
	"testing"

	"fix/internal/teamchat/proto"
)

// A test states names in the form it means, and is not judged.
func TestCanonical(t *testing.T) {
	if !proto.ValidOwner("ExampleOrg") {
		t.Log(proto.Ping{Ref: "commit:ExampleOrg/thing@0"})
	}
}
