package rclonerc

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
)

// Mount is one rclone FUSE mount findmnt lists, with the arguments of the process serving it
// (nil when none does). It is what the rclone reports share (SPEC-rclone-tail D3).
type Mount struct {
	Name       string // the SOURCE up to the first ":"
	RemotePath string // the rest of the SOURCE, without a leading "/"
	Target     string
	Args       []string
}

// Mounts is `findmnt -J -l -o SOURCE,TARGET -t fuse.rclone`, in findmnt's order, each with its
// serving process (the first rclone mount process, in pid order, one of whose arguments after
// the program is exactly the target). findmnt's exit 1 with nothing on stdout is no mount.
func (c *Client) Mounts(ctx context.Context) ([]Mount, error) {
	res, err := c.Runner.Run(ctx, execx.Cmd{
		Name: "findmnt", Args: []string{"-J", "-l", "-o", "SOURCE,TARGET", "-t", "fuse.rclone"},
	})
	var exitErr *execx.ExitError
	if errors.As(err, &exitErr) && exitErr.Code == 1 && len(bytes.TrimSpace(res.Stdout)) == 0 {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("findmnt could not list the rclone mounts: %w", err)
	}
	var listing struct {
		Filesystems []struct {
			Source string `json:"source"`
			Target string `json:"target"`
		} `json:"filesystems"`
	}
	dec := json.NewDecoder(bytes.NewReader(res.Stdout))
	if err = dec.Decode(&listing); err != nil {
		return nil, fmt.Errorf("findmnt could not list the rclone mounts: %w", err)
	}
	if dec.More() {
		return nil, errors.New("findmnt could not list the rclone mounts: more than one JSON value")
	}
	procs, err := c.mountProcesses()
	if err != nil {
		return nil, err
	}
	mounts := make([]Mount, 0, len(listing.Filesystems))
	for _, fsys := range listing.Filesystems {
		parts := strings.SplitN(fsys.Source, ":", 2)
		m := Mount{Name: parts[0], Target: fsys.Target, Args: serving(procs, fsys.Target)}
		if len(parts) == 2 {
			m.RemotePath = strings.TrimPrefix(parts[1], "/")
		}
		mounts = append(mounts, m)
	}
	return mounts, nil
}

// RCAddr is the mount's one --rc-addr value; "" when its process has none or there is no
// process; an error when it has several, since which one it listens on is not established.
func (m Mount) RCAddr() (string, error) {
	switch addrs := FlagValues(m.Args, "--rc-addr"); len(addrs) {
	case 0:
		return "", nil
	case 1:
		return addrs[0], nil
	default:
		return "", errors.New("more than one --rc-addr: " + strings.Join(addrs, " "))
	}
}

// RCError is why an RC call was not answered: Reason is the short text a report prints, Detail
// what goes on stderr for the person fixing it.
type RCError struct {
	Reason string
	Detail string
}

func (f *RCError) Error() string { return f.Reason }

// Ask is Call's answer on stdout, or a *RCError: a missing credential, a refused one (401), or
// any other failure. A call that failed because ctx ended is ctx's error, not a RCError.
func (c *Client) Ask(ctx context.Context, addr, endpoint string) ([]byte, error) {
	res, err := c.Call(ctx, addr, endpoint)
	if err == nil {
		return res.Stdout, nil
	}
	if !c.loaded {
		// Call reads the credential before it runs anything; not loaded is its refusal.
		return nil, &RCError{Reason: "rc credential missing — run play-rclone.yml", Detail: err.Error()}
	}
	if ctxErr := ctx.Err(); ctxErr != nil {
		return nil, ctxErr
	}
	out := strings.TrimSpace(string(res.Stdout) + string(res.Stderr))
	if out == "" {
		out = err.Error()
	}
	if strings.Contains(out, "401") || strings.Contains(out, "Unauthorized") {
		return nil, &RCError{Reason: "rc rejected credentials (401)", Detail: out}
	}
	return nil, &RCError{Reason: "rc unreachable", Detail: out}
}
