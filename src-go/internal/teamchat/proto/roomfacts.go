package proto

import "github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/colour"

// ColourType is a team's colour override (B27, Plan 00217 C8, n3e-D4): main room state under
// key "", written by the admin alone (main's state_default is the closed level). It travels as
// room state, never in the roster, which is the trust record a member refuses on an unknown
// key.
const ColourType = "teamchat.colour"

const colourKey = "colour"

// ColourContent is the override's content: {"colour":"#rrggbb"}, or {} for none.
func ColourContent(hex string) Value {
	if hex == "" {
		return Obj()
	}
	return Obj(M(colourKey, Str(hex)))
}

// DecodeColour reads an override's content: exactly {"colour": <stored hex>} is that colour,
// {} is none (""); anything else is false.
func DecodeColour(content Value) (string, bool) {
	if content.Kind() != KindObject {
		return "", false
	}
	switch m := content.Members(); {
	case len(m) == 0:
		return "", true
	case len(m) == 1 && m[0].Key == colourKey && m[0].Val.Kind() == KindString && colour.Stored(m[0].Val.Str()):
		return m[0].Val.Str(), true
	}
	return "", false
}

// MainColour is the override main's state holds: the admin's event under key "", decoded;
// ("", true) when the admin has written none. Every other sender's event is ignored.
func MainColour(state []StateEvent, server string) (string, bool) {
	if e, ok := adminFact(state, ColourType, server); ok {
		return DecodeColour(e.Content)
	}
	return "", true
}

// MainDescription is the team's description as main's topic carries it (n3e-D5): the admin's
// topic under key "", when it passes ValidDescription and is not the default topic Desired
// writes for a team with none.
func MainDescription(state []StateEvent, server string) (string, bool) {
	e, ok := adminFact(state, topicType, server)
	if !ok {
		return "", false
	}
	topic, isString := e.Content.Get("topic")
	if !isString || topic.Kind() != KindString || !ValidDescription(topic.Str()) || topic.Str() == defaultMainTopic {
		return "", false
	}
	return topic.Str(), true
}

// adminFact is the admin's state event of typ under key "", if main's state holds one.
func adminFact(state []StateEvent, typ, server string) (StateEvent, bool) {
	for _, e := range state {
		if e.Type == typ && e.StateKey == "" && e.Sender == AdminID(server) {
			return e, true
		}
	}
	return StateEvent{}, false
}
