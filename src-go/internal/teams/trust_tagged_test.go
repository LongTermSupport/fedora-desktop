//go:build ccyteamstest

package teams

import (
	"context"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
)

func TestTheTestTagTrustsAPlayOutsideTheCheckout(t *testing.T) {
	r := newOpsRig(t)
	r.ops.Cfg.Play = "/tmp/stub-play"
	r.on("/tmp/stub-play", execx.Result{})
	if err := r.ops.runPlay(context.Background(), []string{"a"}); err != nil {
		t.Fatal(err)
	}
}
