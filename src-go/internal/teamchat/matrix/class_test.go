package matrix

import (
	"context"
	"errors"
	"fmt"
	"testing"
)

// Class is an error's fixed failure class, never its text: what a record may carry.
func TestClass(t *testing.T) {
	for want, err := range map[string]error{
		"rate limited":                         &Error{kind: ErrRateLimited, Detail: "member text"},
		"the homeserver did not answer usably": fmt.Errorf("wrapped: %w", &Error{kind: ErrUnreachable}),
		"the homeserver refused":               &Error{},
		"cancelled":                            fmt.Errorf("x: %w", context.Canceled),
		"timed out":                            context.DeadlineExceeded,
		"not a homeserver answer":              errors.New("member text"),
	} {
		if got := Class(err); got != want {
			t.Errorf("Class(%v) = %q, want %q", err, got, want)
		}
	}
}
