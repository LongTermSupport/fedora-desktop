package state

import "testing"

const conversationDoc = `{"v":1,"run":"` + sampleRun + `","session_id":"` + sampleSessionID + `","source":"resume","written_ms":1760000000000}`

func TestDecodeConversation(t *testing.T) {
	accepts(t, DecodeConversation, conversationDoc)
	for _, source := range sources {
		accepts(t, DecodeConversation, edit(t, conversationDoc, `"resume"`, `"`+source+`"`))
	}
	c, err := DecodeConversation([]byte(conversationDoc))
	if err != nil || c.SessionID != sampleSessionID || c.Source != SourceResume || c.Run.String() != sampleRun {
		t.Fatalf("DecodeConversation = %+v, %v", c, err)
	}
	strictRefusals(t, conversationFile, conversationDoc, "written_ms", MaxConversation, DecodeConversation)
	refusesAll(t, conversationFile, conversationDoc, DecodeConversation, []refusal{
		{`"session_id":"` + sampleSessionID + `"`, `"session_id":"SECRET"`, CodeSchema},
		{`"session_id":"` + sampleSessionID + `"`, `"session_id":null`, CodeSchema},
		{`"resume"`, `"SECRET"`, CodeSchema},
		{`"written_ms":1760000000000`, `"written_ms":-424242`, CodeSchema},
		{`"written_ms":1760000000000`, `"written_ms":253402300800000`, CodeSchema},
	})
}

func TestConversationRoundTrip(t *testing.T) {
	c, err := DecodeConversation([]byte(conversationDoc))
	check(t, err)
	roundTrips(t, c, MaxConversation, DecodeConversation)
}
