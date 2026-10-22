package declare

import (
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
)

// A declared container is a paths.ContainerName, so every name the declaration accepts is one
// the host can derive a path from, and no other.
func TestContainerIsPathsGrammar(t *testing.T) {
	for _, s := range []string{"box-1", "Box_2.dev", "9", strings.Repeat("a", 64), "", ".", "..", "-a", "a/b", strings.Repeat("a", 65)} {
		_, err := paths.ParseContainerName(s)
		if validContainer(s) != (err == nil) {
			t.Errorf("container %q: declare accepts %v, paths.ParseContainerName says %v", s, validContainer(s), err)
		}
	}
}
