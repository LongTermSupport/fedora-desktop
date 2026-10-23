package twicecheck

import (
	"testing"

	"fix/internal/leakcheck"
)

type world struct{ t *testing.T }

// newWorld registers the test's leak check first, as the engine's world does.
func newWorld(t *testing.T) *world {
	t.Helper()
	leakcheck.Check(t)
	return buildWorld(t)
}

func buildWorld(t *testing.T) *world {
	t.Cleanup(func() {})
	return &world{t: t}
}

// another is a second world in a test whose check the first registered.
func (w *world) another() *world { return buildWorld(w.t) }

// newDeliveryWorld reaches the check through newWorld.
func newDeliveryWorld(t *testing.T) *world { return newWorld(t) }

// namedWorld carries the test on its second parameter.
func namedWorld(name string, t *testing.T) *world {
	t.Log(name)
	return newDeliveryWorld(t)
}

// TestLiveNeedsEveryFact is the originating shape: two worlds, each registering the check.
func TestLiveNeedsEveryFact(t *testing.T) {
	newDeliveryWorld(t)
	newDeliveryWorld(t) // want `leakcheck.Check registered again on t, through newDeliveryWorld`
}

func TestMixedHelpers(t *testing.T) {
	newWorld(t)
	namedWorld("again", t) // want `leakcheck.Check registered again on t, through namedWorld`
}

func TestDirectTwice(t *testing.T) {
	leakcheck.Check(t)
	leakcheck.Check(t) // want `leakcheck.Check registered again on t, through leakcheck.Check`
}

func TestInALoop(t *testing.T) {
	for range 4 {
		newWorld(t) // want `leakcheck.Check registered again on t, through newWorld`
	}
}

func TestInALiteral(t *testing.T) {
	func() {
		newWorld(t)
		newWorld(t) // want `leakcheck.Check registered again on t, through newWorld`
	}()
}

// helperTwice registers the check twice on the test it is given.
func helperTwice(tb testing.TB) {
	leakcheck.Check(tb)
	leakcheck.Check(tb) // want `leakcheck.Check registered again on tb, through leakcheck.Check`
}

func TestOnceWithAnother(t *testing.T) {
	w := newWorld(t)
	w.another()
	buildWorld(t)
}

func TestSubtests(t *testing.T) {
	for _, name := range []string{"a", "b"} {
		t.Run(name, func(t *testing.T) {
			newWorld(t)
		})
	}
}

func TestTwoTests(t *testing.T) {
	other := &testing.T{}
	newWorld(t)
	newWorld(other)
}
