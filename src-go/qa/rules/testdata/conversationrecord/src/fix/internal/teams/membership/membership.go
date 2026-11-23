// Package membership stands for v1's per-session directory.
package membership

// SessionDir is v1's per-session directory in a checkout.
func SessionDir(checkout, session string) string {
	return checkout + "/.claude/ccy/pingbus/sessions/" + session
}
