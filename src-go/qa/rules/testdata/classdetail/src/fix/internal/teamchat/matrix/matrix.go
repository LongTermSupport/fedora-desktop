// Package matrix is teamchat's Matrix client as the rule sees it: the class and the cause.
package matrix

// Class is a failed call's class as fixed text.
func Class(_ error) string { return "the homeserver did not answer usably" }

// Cause is the class with the failed call's detail.
func Cause(err error) string { return Class(err) + " (" + err.Error() + ")" }

// Error is a failed call.
type Error struct{ Detail string }

// Class is a method of the same name, which is not the class function.
func (e *Error) Class() string { return "x" }

func (e *Error) Error() string { return e.Detail }
