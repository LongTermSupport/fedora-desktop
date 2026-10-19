// Package notab takes Tab by hand with no binding for it: nothing draws a hint that Tab does
// anything, which is what makes Tab navigation and not a hidden action.
package notab

import tea "charm.land/bubbletea/v2"

// update completes on Tab, unsaid.
func update(press tea.KeyPressMsg) string {
	if press.Code == tea.KeyTab { // want `Tab taken by hand with no key.Binding for it`
		return "complete"
	}
	if press.String() == "shift+tab" { // want `Tab taken by hand with no key.Binding for it`
		return "back"
	}
	if press.String() == "left" {
		return "editing"
	}
	return ""
}
