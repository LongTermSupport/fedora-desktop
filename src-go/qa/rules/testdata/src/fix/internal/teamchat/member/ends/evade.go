package ends

import (
	"context"
	"sync"

	nt "fix/internal/teamchat/notice"
)

// The ways round the turn: each still reads or posts with nothing ordering it.

// An import alias does not hide the call.
func aliased(ctx context.Context, n nt.Client) error {
	_, err := nt.LatestOwn(ctx, n, "main") // want `lifecycle notice read or posted outside the team's turn: notice.LatestOwn`
	return err
}

// A method value escapes the turn's ordering wherever it is called.
func methodValue(n nt.Client) func(context.Context, string, string, string) (string, error) {
	return n.Send // want `lifecycle notice read or posted outside the team's turn: Send`
}

// A helper's method value is the helper.
func (e *ends) helperValue() func(context.Context, nt.Client, string) error {
	return e.post // want `lifecycle notice read or posted outside the team's turn: post`
}

// A goroutine started inside the turn outlives it.
func (e *ends) goInside(ctx context.Context, n nt.Client) error {
	return e.turn(ctx, "t", func() error {
		go func() {
			_, err := n.Send(ctx, "main", "tc", "back") // want `lifecycle notice read or posted outside the team's turn: Send`
			use(err)
		}()
		return nil
	})
}

// So does one a WaitGroup starts.
func (e *ends) wgInside(ctx context.Context, n nt.Client) error {
	var wg sync.WaitGroup
	err := e.turn(ctx, "t", func() error {
		wg.Go(func() {
			use(e.post(ctx, n, "left")) // want `lifecycle notice read or posted outside the team's turn: post`
		})
		return nil
	})
	wg.Wait()
	return err
}

// A read through the client directly is a decision too.
func direct(ctx context.Context, n nt.Client) error {
	_, err := n.Messages(ctx, "main") // want `lifecycle notice read or posted outside the team's turn: Messages`
	return err
}

// A chain of helpers is followed to the read.
func (e *ends) Leave(ctx context.Context, n nt.Client) error {
	return e.outer(ctx, n) // want `lifecycle notice read or posted outside the team's turn: outer`
}

func (e *ends) outer(ctx context.Context, n nt.Client) error { return e.post(ctx, n, "left") }

func use(error) {}
