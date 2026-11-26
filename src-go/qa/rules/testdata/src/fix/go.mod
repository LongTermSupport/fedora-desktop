module fix

go 1.26.0

require (
	charm.land/bubbles/v2 v2.2.1
	charm.land/bubbletea/v2 v2.1.0
	golang.org/x/sys v0.47.0
)

replace (
	charm.land/bubbles/v2 => ../charm.land/bubbles/v2
	charm.land/bubbletea/v2 => ../charm.land/bubbletea/v2
	golang.org/x/sys => ../golang.org/x/sys
)
