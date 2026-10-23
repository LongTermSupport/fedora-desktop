// Package elsewhere is outside the ends package: its posts are not the rule's to judge (the
// host's calls, ordered across processes by their transaction ids).
package elsewhere

import (
	"context"

	"fix/internal/teamchat/notice"
)

func hostCall(ctx context.Context, n notice.Client) error {
	return notice.Disconnected(ctx, n, "main")
}
