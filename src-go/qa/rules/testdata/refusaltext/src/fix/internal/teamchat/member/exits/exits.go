// Package exits is the member's error object, as teamchat's exits package has it.
package exits

// Template is a message with one {} per fill.
type Template string

// Fill is one checked name for a template.
type Fill struct{ text string }

// Error is a verb's failure.
type Error struct{ Code, Message string }

func (e *Error) Error() string { return e.Message }

// Words is a verb or slice name.
func Words(s string) Fill { return Fill{s} }

// Name is an identifier.
func Name(s string) Fill { return Fill{s} }

// Format is a template filled as one fill.
func Format(t Template, fills ...Fill) Fill { return Fill{string(t) + fills[0].text} }

// ExitOf is the exit whose set holds code.
func ExitOf(code string) (int, bool) { return len(code), code != "" }

// New is the error for code.
func New(code string, t Template, fills ...Fill) *Error {
	return &Error{Code: code, Message: string(t) + fills[0].text}
}
