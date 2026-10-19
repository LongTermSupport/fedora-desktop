// Package tea is a stand-in for Bubble Tea v2's key messages, enough for the rules' fixtures.
package tea

// Msg is any message.
type Msg any

// Key is a key event.
type Key struct {
	Code rune
	Text string
}

// KeyPressMsg is a key pressed.
type KeyPressMsg Key

// String is the key as text, "ctrl+x".
func (k KeyPressMsg) String() string { return k.Text }

// Keystroke is the key as text with its modifiers.
func (k KeyPressMsg) Keystroke() string { return k.Text }

// KeyReleaseMsg is a key released.
type KeyReleaseMsg Key

// KeyMsg is either.
type KeyMsg interface{ String() string }

// Some of the key codes.
const (
	KeyTab       rune = 9
	KeyEnter     rune = 13
	KeyEscape    rune = 27
	KeyBackspace rune = 127
	KeyF1        rune = 0x10001
)
