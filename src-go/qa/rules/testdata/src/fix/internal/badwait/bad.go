// Package badwait sets WaitDelay and never decides what ErrWaitDelay means.
package badwait

import (
	"bytes"
	"os/exec"
	"time"
)

// dropsTheOutput is bug B25: any error but an exit status is a failed run, so the output a
// finished command wrote is thrown away when a child holds its pipe.
func dropsTheOutput() ([]byte, error) {
	cmd := exec.Command("ls")
	cmd.WaitDelay = time.Second // want `WaitDelay set and exec.ErrWaitDelay never named`
	var out bytes.Buffer
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

// configure sets the delay and leaves the decision to a caller: the decision belongs beside
// the setting, which is where a reader looks for it.
func configure(cmd *exec.Cmd) {
	cmd.WaitDelay = time.Second // want `WaitDelay set and exec.ErrWaitDelay never named`
}
