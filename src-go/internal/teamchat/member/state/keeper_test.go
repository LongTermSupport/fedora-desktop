package state

import (
	"strings"
	"testing"
)

const keeperRunning = `{"v":1,"run":"` + sampleRun + `","link":"running","why":null,"restarts":0,"next_ms":null,` +
	`"kind":"ccy","session":"s1","desired":"applied"}`

func TestDecodeKeeper(t *testing.T) {
	restarting := edit(t, edit(t, keeperRunning, `"link":"running"`, `"link":"restarting"`), `"next_ms":null`, `"next_ms":5`)
	failed := edit(t, edit(t, keeperRunning, `"link":"running"`, `"link":"failed"`), `"why":null`, `"why":"crash-loop"`)
	config := edit(t, failed, `"crash-loop"`, `"config"`)
	host := edit(t, edit(t, keeperRunning, `"ccy"`, `"host"`), `"session":"s1"`, `"session":null`)
	other := edit(t, host, `"host"`, `"other"`)
	lxc := edit(t, edit(t, keeperRunning, `"ccy"`, `"lxc"`), `"desired":"applied"`, `"desired":"refused"`)
	accepts(t, DecodeKeeper, keeperRunning, restarting, failed, config, host, other, lxc)
	k, err := DecodeKeeper([]byte(restarting))
	if err != nil || k.Link != LinkRestarting || *k.NextMS != 5 || k.Session.String() != "s1" {
		t.Fatalf("DecodeKeeper = %+v, %v", k, err)
	}
	strictRefusals(t, keeperFile, keeperRunning, "restarts", MaxKeeper, DecodeKeeper)
	refusesAll(t, keeperFile, keeperRunning, DecodeKeeper, []refusal{
		{`"running"`, `"SECRET"`, CodeSchema},
		{`"why":null`, `"why":"config"`, CodeSchema},
		{`"next_ms":null`, `"next_ms":5`, CodeSchema},
		{`"restarts":0`, `"restarts":-1`, CodeSchema},
		{`"ccy"`, `"SECRET"`, CodeSchema},
		{`"session":"s1"`, `"session":null`, CodeSchema},
		{`"session":"s1"`, `"session":"../SECRET"`, CodeSchema},
		{`"applied"`, `"SECRET"`, CodeSchema},
		{`"run":"` + sampleRun + `"`, `"run":null`, CodeSchema},
	})
	refusesAll(t, keeperFile, failed, DecodeKeeper, []refusal{
		{`"why":"crash-loop"`, `"why":null`, CodeSchema},
		{`"crash-loop"`, `"SECRET"`, CodeSchema},
	})
	refusesAll(t, keeperFile, restarting, DecodeKeeper, []refusal{
		{`"next_ms":5`, `"next_ms":null`, CodeSchema},
		{`"next_ms":5`, `"next_ms":-424242`, CodeSchema},
	})
	refusesAll(t, keeperFile, host, DecodeKeeper, []refusal{
		{`"session":null`, `"session":"s1"`, CodeSchema},
	})
}

// TestKeeperCap holds the largest keeper.json within its cap.
func TestKeeperCap(t *testing.T) {
	doc := edit(t, keeperRunning, `"session":"s1"`, `"session":"`+strings.Repeat("s", 128)+`"`)
	doc = edit(t, doc, `"link":"running"`, `"link":"restarting"`)
	doc = edit(t, doc, `"next_ms":null`, `"next_ms":9007199254740991`)
	doc = edit(t, doc, `"restarts":0`, `"restarts":9007199254740991`)
	if len(doc) > MaxKeeper/2 {
		t.Fatalf("the largest keeper.json is %d bytes", len(doc))
	}
	accepts(t, DecodeKeeper, doc)
}

func TestKeeperRoundTrip(t *testing.T) {
	k, err := DecodeKeeper([]byte(keeperRunning))
	check(t, err)
	roundTrips(t, k, MaxKeeper, DecodeKeeper)
}
