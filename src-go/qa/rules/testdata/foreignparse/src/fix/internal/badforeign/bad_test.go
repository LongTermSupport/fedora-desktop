package badforeign

import (
	"os"
	"testing"

	"fix/internal/teamchat/proto"
)

// A test parses what it planted; tests are not judged.
func TestPlanted(t *testing.T) {
	if _, err := proto.Parse([]byte(os.Getenv("PLANTED"))); err != nil {
		t.Log(err)
	}
}
