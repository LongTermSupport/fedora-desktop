//go:build ccyteamstest

package ops

import (
	"context"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
)

func TestTheTestTagTrustsAPlayOutsideTheCheckout(t *testing.T) {
	r := newOpsRig(t)
	r.ops.Cfg.Play = "/tmp/stub-play"
	r.on("/tmp/stub-play", execx.Result{})
	err := r.ops.Locked(context.Background(), "apply a", func(ctx context.Context) error {
		return r.ops.runPlay(ctx, []string{"a"})
	})
	if err != nil {
		t.Fatal(err)
	}
}
