package badwait

import (
	"bytes"
	"errors"
	"os/exec"
	"time"
)

// keepsTheOutput names the case and keeps what was written.
func keepsTheOutput() ([]byte, error) {
	cmd := exec.Command("ls")
	cmd.WaitDelay = time.Second
	var out bytes.Buffer
	cmd.Stdout = &out
	err := cmd.Run()
	if errors.Is(err, exec.ErrWaitDelay) {
		return out.Bytes(), nil
	}
	if err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

// noDelay never sets WaitDelay, so there is nothing to decide.
func noDelay() error {
	cmd := exec.Command("ls")
	cmd.Dir = "/"
	return cmd.Run()
}
