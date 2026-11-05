//go:build busfailpoints

package engine

import "github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/failpoint"

// hitPoint is the engine's failpoint: the named point of TEAMCHAT_FAILPOINTS (failpoint.Hit).
func hitPoint(p point) { failpoint.Hit(p.name()) }
