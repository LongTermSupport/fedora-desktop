// Package a is the fixture of project_defer_assign_lost.
package a

import (
	"errors"
	"os"
)

// VerifyDir is the originating form: unnamed results, so the joined Close error is lost.
func VerifyDir(dir string) ([]byte, error) {
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, err
	}
	defer func() { err = errors.Join(err, root.Close()) }() // want `a deferred function assigns err, which is not a named result \(project_defer_assign_lost\)`
	return nil, nil
}

// Counted counts in a deferred function, too late for the return.
func Counted() int {
	n := 0
	defer func() { n++ }() // want `a deferred function assigns n, which is not a named result \(project_defer_assign_lost\)`
	return n
}

// Nested loses the error in a function literal, whose own results are unnamed.
func Nested() error {
	run := func() error {
		f, err := os.Open("x")
		if err != nil {
			return err
		}
		defer func() { err = f.Close() }() // want `a deferred function assigns err, which is not a named result \(project_defer_assign_lost\)`
		return nil
	}
	return run()
}

// Named is the fix: err is a named result, so the deferred assignment is returned.
func Named(dir string) (data []byte, err error) {
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, err
	}
	defer func() { err = errors.Join(err, root.Close()) }()
	return nil, nil
}

// NamedInLiteral is clean: the enclosing function is the literal, whose result is named.
func NamedInLiteral() error {
	run := func() (err error) {
		f, err := os.Open("x")
		if err != nil {
			return err
		}
		defer func() { err = errors.Join(err, f.Close()) }()
		return nil
	}
	return run()
}

// Box holds an error a deferred function can set.
type Box struct{ Err error }

// NotLocal sets a field and a pointer's target, which outlive the call.
func NotLocal(b *Box, p *error) {
	f, err := os.Open("x")
	if err != nil {
		return
	}
	defer func() {
		closeErr := f.Close()
		b.Err = closeErr
		*p = closeErr
	}()
}

// DeferredNamedFunc defers a named function, not a literal.
func DeferredNamedFunc(b *Box) {
	f, err := os.Open("x")
	if err != nil {
		return
	}
	defer closeInto(f, b)
}

func closeInto(f *os.File, b *Box) { b.Err = f.Close() }
