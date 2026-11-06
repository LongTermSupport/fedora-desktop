package badrestart

import (
	"testing"

	"fix/internal/fakehs"
)

// The originating shape (fedora-desktop#149): the hook restarts the fake server on the code's
// goroutine while the test's goroutine posts to it.
func TestTheHookRestarts(t *testing.T) {
	srv := &fakehs.Server{}
	e := Engine{}
	writes := 0
	e.Hook = func() error {
		if writes++; writes == 2 {
			return srv.Restart() // want `fakehs.Server.Restart in a function literal`
		}
		return nil
	}
}

func TestAStopInAGoroutineAndAGoStatement(t *testing.T) {
	srv := &fakehs.Server{}
	done := make(chan error, 1)
	go func() { done <- srv.Stop() }() // want `fakehs.Server.Stop in a function literal`
	go srv.Restart()                   // want `fakehs.Server.Restart in a go statement`
	<-done
}

func TestANestedLiteralInACleanupIsJudged(t *testing.T) {
	srv := &fakehs.Server{}
	t.Cleanup(func() {
		hook := func() error { return srv.Stop() } // want `fakehs.Server.Stop in a function literal`
		if err := hook(); err != nil {
			t.Error(err)
		}
	})
}

// The fixed form: the hook tells the test, and the test restarts on its own goroutine.
func TestTheTestRestarts(t *testing.T) {
	srv := &fakehs.Server{}
	writing := make(chan struct{})
	e := Engine{Hook: func() error {
		close(writing)
		return nil
	}}
	go func() {
		if err := e.Hook(); err != nil {
			t.Error(err)
		}
	}()
	<-writing
	if err := srv.Restart(); err != nil {
		t.Fatal(err)
	}
}

// A cleanup, a subtest's body and a deferred stop run on the goroutine that then owns the
// server, or after its last use; Start, Close and a Restart of a type that is not a fake are not
// judged.
func TestTheCleanShapes(t *testing.T) {
	srv := &fakehs.Server{}
	defer func() {
		if err := srv.Stop(); err != nil {
			t.Error(err)
		}
	}()
	t.Cleanup(func() {
		if err := srv.Stop(); err != nil {
			t.Error(err)
		}
	})
	t.Run("down", func(t *testing.T) {
		if err := srv.Stop(); err != nil {
			t.Fatal(err)
		}
	})
	go func() {
		if err := srv.Start(); err != nil {
			t.Error(err)
		}
		if err := srv.Close(); err != nil {
			t.Error(err)
		}
	}()
	real := &Server{}
	go func() {
		if err := real.Restart(); err != nil {
			t.Error(err)
		}
	}()
}
