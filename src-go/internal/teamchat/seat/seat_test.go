package seat

import (
	"errors"
	"os"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
)

// Open needs every part of its config, and makes the store root 0700.
func TestOpenNeedsEveryPart(t *testing.T) {
	w := newWorld(t)
	for name, change := range map[string]func(*Config){
		"no clock":      func(c *Config) { c.Clock = nil },
		"relative home": func(c *Config) { c.StateHome = "state" },
		"relative run":  func(c *Config) { c.RuntimeDir = "run" },
		"no checkout":   func(c *Config) { c.Checkout = paths.CheckoutKey{} },
	} {
		cfg := w.config()
		change(&cfg)
		if s, err := Open(cfg); !errors.Is(err, ErrConfig) {
			t.Errorf("%s: %v", name, err)
			if err == nil {
				t.Error(s.Close())
			}
		}
	}
	if mode(t, w.store.store.Root()).Perm() != 0o700 {
		t.Errorf("the store root is %v", mode(t, w.store.store.Root()))
	}
}

// SW2: a store root that is not the owner's 0700 directory is refused.
func TestOpenRefusesAnOpenRoot(t *testing.T) {
	w := newWorld(t)
	if err := os.Chmod(w.store.store.Root(), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(w.config()); !errors.Is(err, ErrConfig) {
		t.Errorf("a 0755 store root: %v", err)
	}
}
