// Package key is a stand-in for Bubbles v2's key bindings, enough for the rules' fixtures.
package key

// Binding is a key binding and its help.
type Binding struct{ keys []string }

// BindingOpt sets an option of a binding.
type BindingOpt func(*Binding)

// NewBinding is a binding with the options.
func NewBinding(opts ...BindingOpt) Binding {
	b := Binding{}
	for _, o := range opts {
		o(&b)
	}
	return b
}

// WithKeys names the keys.
func WithKeys(keys ...string) BindingOpt { return func(b *Binding) { b.keys = keys } }

// WithHelp is the key and the words the help shows for it.
func WithHelp(_, _ string) BindingOpt { return func(*Binding) {} }

// Matches is whether the message is one of the bindings' keys.
func Matches(msg any, b ...Binding) bool { return len(b) > 0 && msg != nil }
