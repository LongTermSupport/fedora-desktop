// Package outsiderefusal is outside teamchat, so it is not judged.
package outsiderefusal

import (
	"fix/internal/teamchat/member/exits"
	"fix/internal/teamchat/proto"
)

const msgRefused exits.Template = "refused ({})"

func outside(re *proto.RefusalError) error {
	return exits.New("ref", msgRefused, exits.Words(re.Reason))
}
