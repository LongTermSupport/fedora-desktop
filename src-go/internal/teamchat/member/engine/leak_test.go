package engine

import (
	"os"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/leakcheck"
)

func TestMain(m *testing.M) { os.Exit(leakcheck.Tests(m).Run()) }
