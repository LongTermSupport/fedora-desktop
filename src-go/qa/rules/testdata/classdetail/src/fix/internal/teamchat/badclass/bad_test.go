package badclass

import (
	"testing"

	"fix/internal/teamchat/matrix"
)

func TestInATest(t *testing.T) {
	t.Logf("down: %s", matrix.Class(nil)) // want `a failed call's class printed without its detail`
}
