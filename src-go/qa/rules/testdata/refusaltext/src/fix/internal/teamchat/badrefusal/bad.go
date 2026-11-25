package badrefusal

import (
	"fix/internal/teamchat/member/exits"
	"fix/internal/teamchat/proto"
)

const (
	msgRefused exits.Template = "the send was refused ({}); fix the message"
	msgRef     exits.Template = "the verb needs --ref; {}"
	msgPlain   exits.Template = "the reference is missing{}"
)

func asFill(re *proto.RefusalError) error {
	return exits.New("ref", msgRefused, exits.Words(re.Reason)) // want `a refusal's reason code in its message`
}

func asName(re *proto.RefusalError) error {
	return exits.New(re.Reason, msgRefused, exits.Name("why: "+re.Reason)) // want `a refusal's reason code in its message`
}

func copied(re *proto.RefusalError) error {
	why := re.Reason
	return exits.New("ref", msgRefused, exits.Words(why)) // want `a refusal's reason code in its message`
}

func formatted(re proto.RefusalError) exits.Fill {
	return exits.Format(msgRef, exits.Words(re.Reason)) // want `a refusal's reason code in its message`
}

func assigned(re *proto.RefusalError) error {
	var why string
	why = re.Reason
	return exits.New("ref", msgRefused, exits.Words(why)) // want `a refusal's reason code in its message`
}

func good(re *proto.RefusalError, ref string) error {
	code := re.Reason
	if _, known := exits.ExitOf(re.Reason); !known {
		code = "schema"
	}
	switch re.Reason {
	case "ref":
		return exits.New(code, msgRef, exits.Name(ref))
	}
	return exits.New(code, msgPlain, exits.Name(""))
}
