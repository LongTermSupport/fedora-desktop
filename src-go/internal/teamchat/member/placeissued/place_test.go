package placeissued

import (
	"errors"
	"maps"
	"os"
	"strings"
	"testing"
	"testing/iotest"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/exits"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/seat"
)

func TestPlaceMakesTheSeatInThePool(t *testing.T) {
	w := newWorld(t)
	w.mustPlace(passwordDoc(t, secretOne))
	pool := w.pool()
	if got := w.text(pool.Password()); got != secretOne {
		t.Errorf("the pool password is %d bytes, want the issued one", len(got))
	}
	wantJSON, err := bundleAt(t, testBase).Encode()
	if err != nil {
		t.Fatal(err)
	}
	if got := w.text(pool.Bundle()); got != string(wantJSON) {
		t.Errorf("seat.json %s\nwant     %s", got, wantJSON)
	}
	for _, file := range []string{pool.Password(), pool.Bundle(), w.store.PoolCheckoutRecord(w.key)} {
		if perm := w.perm(file); perm != 0o600 {
			t.Errorf("%s is mode %o, want 600", file, perm)
		}
	}
	for _, dir := range []string{pool.Dir(), w.store.PoolTeam(w.key, w.team), w.store.PoolCheckout(w.key), w.store.Pool(), w.store.Root()} {
		if perm := w.perm(dir); perm != 0o700 {
			t.Errorf("%s is mode %o, want 700", dir, perm)
		}
	}
	if w.exists(pool.Token()) {
		t.Error("a password seat holds a token file")
	}
	if got := w.text(w.store.PoolCheckoutRecord(w.key)); !strings.Contains(got, `"ops"`) || !strings.Contains(got, testBase) {
		t.Errorf("checkout.json %s does not record the team on its homeserver", got)
	}
	if left := w.leftovers(); len(left) != 0 {
		t.Errorf("left %v", left)
	}
	if len(w.reach.asked) != 1 || w.reach.asked[0] != testBase {
		t.Errorf("the homeserver was asked %v, want the new seat's URL once", w.reach.asked)
	}
	w.lockIsFree()
}

func TestPlaceTakesTheAnswerOfAMemberVerb(t *testing.T) {
	bare, answer := newWorld(t), newWorld(t)
	bare.mustPlace(passwordDoc(t, secretOne))
	answer.mustPlace(answerDoc(t, passwordDoc(t, secretOne), map[string]any{"notes": []string{"a note"}}))
	if got, want := answer.text(answer.pool().Bundle()), bare.text(bare.pool().Bundle()); got != want {
		t.Errorf("seat.json %s\nwant     %s", got, want)
	}
	if got := answer.text(answer.pool().Password()); got != secretOne {
		t.Error("the answer's credential was not placed")
	}
}

// The same document placed twice is accepted, not refused: the second run replaces the record
// with an identical one and quarantines the first.
func TestPlaceTheSameDocumentTwiceIsAccepted(t *testing.T) {
	w := newWorld(t)
	doc := passwordDoc(t, secretOne)
	w.mustPlace(doc)
	first := w.files(w.pool().Dir())
	w.mustPlace(doc)
	if second := w.files(w.pool().Dir()); !maps.Equal(first, second) {
		t.Errorf("the pool changed:\n%v\n%v", first, second)
	}
	if got := w.quarantined(); len(got) != 1 {
		t.Errorf("quarantine holds %v, want the first record", got)
	}
	if holding := w.holding(secretOne); len(holding) != 1 {
		t.Errorf("the credential is in %v, want only the pool's", holding)
	}
	if len(w.reach.asked) != 1 {
		t.Errorf("the homeserver was asked %v: an existing seat needs no check", w.reach.asked)
	}
}

func TestPlaceReplacesTheRecordAndKeepsNoOldCredential(t *testing.T) {
	w := newWorld(t)
	w.mustPlace(passwordDoc(t, secretOne))
	w.mustPlace(passwordDoc(t, secretTwo))
	if got := w.text(w.pool().Password()); got != secretTwo {
		t.Error("the pool does not hold the new credential")
	}
	if got := w.holding(secretOne); len(got) != 0 {
		t.Errorf("the replaced credential is still in %v", got)
	}
	if got := w.quarantined(); len(got) != 1 {
		t.Errorf("quarantine holds %v, want the replaced record", got)
	}
}

func TestPlaceMovesACrashImageToQuarantine(t *testing.T) {
	w := newWorld(t)
	id, err := paths.ParseClaimID("0123456789abcdef")
	if err != nil {
		t.Fatal(err)
	}
	crashed := w.store.PoolClaim(w.key, w.team, w.name, id)
	w.writeFile(crashed.Password(), secretOne)
	w.mustPlace(passwordDoc(t, secretTwo))
	if left := w.leftovers(); len(left) != 0 {
		t.Errorf("left %v", left)
	}
	if got := w.holding(secretOne); len(got) != 0 {
		t.Errorf("the crashed placement's credential is still in %v", got)
	}
	if got := w.quarantined(); len(got) != 1 {
		t.Errorf("quarantine holds %v, want the crash image", got)
	}
	if got := w.text(w.pool().Password()); got != secretTwo {
		t.Error("the pool does not hold the placed credential")
	}
}

func TestPlaceWithAnInjectedTokenCredentialPlacesTheToken(t *testing.T) {
	w := newWorld(t)
	w.deps.Credential = tokenPool{}
	w.mustPlace(issuedDoc(t, testBase, seat.KindToken, secretOne))
	pool := w.pool()
	if got := w.text(pool.Token()); got != secretOne {
		t.Error("the token file does not hold the issued token")
	}
	if perm := w.perm(pool.Token()); perm != 0o600 {
		t.Errorf("the token is mode %o, want 600", perm)
	}
	if w.exists(pool.Password()) {
		t.Error("a token seat holds a password file")
	}
}

func TestPlaceRefusesATokenForThePasswordStoreNamingTheKinds(t *testing.T) {
	w := newWorld(t)
	w.refuses(issuedDoc(t, testBase, seat.KindToken, secretOne), refusalWant{
		exits.ExitConfig, exits.CodeBundle, string(msgInvalid), "a token, not this pool's password",
	})
	if w.exists(w.pool().Dir()) {
		t.Error("a seat was made")
	}
}

func TestPlaceRefusesWhatReadRefusesAndTouchesNothing(t *testing.T) {
	for _, c := range refusals(t) {
		w := newWorld(t)
		code, stderr, obj := w.run(c.doc)
		if code != exits.ExitConfig || obj.Code != exits.CodeBundle || obj.Message != string(c.msg) {
			t.Errorf("%s: exit %d object %+v", c.name, code, obj)
		}
		for _, leaked := range []string{secretOne, secretTwo, "leakmarker"} {
			if strings.Contains(stderr, leaked) {
				t.Errorf("%s: stderr shows %q", c.name, leaked)
			}
		}
		for _, made := range []string{w.deps.StateHome, w.deps.RuntimeDir} {
			if w.exists(made) {
				t.Errorf("%s: a refused document made %s", c.name, made)
			}
		}
	}
}

func TestPlaceRefusesATeamOnAnotherURLNamingBoth(t *testing.T) {
	w := newWorld(t)
	w.mustPlace(passwordDoc(t, secretOne))
	other := "http://127.0.0.1:9448"
	stderr := w.refuses(issuedDoc(t, other, seat.KindPassword, secretTwo), refusalWant{
		exits.ExitConfig, exits.CodeBundle, string(msgClash), testBase,
	})
	if !strings.Contains(stderr, other) {
		t.Errorf("stderr %q does not name the second URL", stderr)
	}
	if got := w.text(w.pool().Password()); got != secretOne {
		t.Error("the refusal changed the seat")
	}
}

func TestPlaceRefusesATeamOnAnotherServerNameNamingBoth(t *testing.T) {
	w := newWorld(t)
	w.writeFile(w.store.PoolCheckoutRecord(w.key), `{"v":1,"teams":{"ops":{"server_name":"other.example.test","base_url":"`+testBase+`"}}}`)
	stderr := w.refuses(passwordDoc(t, secretOne), refusalWant{
		exits.ExitConfig, exits.CodeBundle, string(msgClash), "other.example.test",
	})
	if !strings.Contains(stderr, paths.ServerName(w.team)) {
		t.Errorf("stderr %q does not name the bundle's server name", stderr)
	}
	if w.exists(w.pool().Dir()) {
		t.Error("a seat was made")
	}
}

func TestPlaceRefusesWhileAnotherRunHoldsTheAllocationLock(t *testing.T) {
	w := newWorld(t)
	w.holdLock()
	w.refuses(passwordDoc(t, secretOne), refusalWant{
		exits.ExitBusy, exits.CodeSeatHeld, string(msgBusy), "allocation lock",
	})
	if len(w.reach.asked) != 0 {
		t.Errorf("the homeserver was asked %v before the lock was held", w.reach.asked)
	}
}

func TestPlaceRefusesAStoreRootThatIsNotPrivate(t *testing.T) {
	w := newWorld(t)
	if err := os.MkdirAll(w.store.Root(), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(w.store.Root(), 0o755); err != nil {
		t.Fatal(err)
	}
	w.refuses(passwordDoc(t, secretOne), refusalWant{
		exits.ExitConfig, exits.CodeStore, string(msgStore), "mode 0700",
	})
}

func TestPlaceRefusesAPoolDirectoryThatIsNotPrivate(t *testing.T) {
	w := newWorld(t)
	w.mustPlace(passwordDoc(t, secretOne))
	if err := os.Chmod(w.store.Pool(), 0o755); err != nil {
		t.Fatal(err)
	}
	w.refuses(passwordDoc(t, secretTwo), refusalWant{
		exits.ExitConfig, exits.CodeStore, string(msgStore), "mode 0700",
	})
}

func TestPlaceRefusesANewSeatWhoseHomeserverDoesNotAnswer(t *testing.T) {
	w := newWorld(t)
	w.reach.err = errors.New("connection refused")
	unreachable := exits.New(exits.CodeUnreachable, nil, msgUnreachable, exits.Name(testBase)).Message
	w.refuses(passwordDoc(t, secretOne), refusalWant{exits.ExitUnreachable, exits.CodeUnreachable, unreachable, testBase})
	if w.exists(w.pool().Dir()) || w.exists(w.store.PoolCheckoutRecord(w.key)) {
		t.Error("an unreachable seat was placed")
	}
}

func TestPlaceRefusesAHeldSeat(t *testing.T) {
	w := newWorld(t)
	w.mustPlace(passwordDoc(t, secretOne))
	if err := os.MkdirAll(w.pool().Held(), 0o700); err != nil {
		t.Fatal(err)
	}
	w.refuses(passwordDoc(t, secretTwo), refusalWant{
		exits.ExitBusy, exits.CodeSeatHeld, string(msgHeld), "release it first",
	})
	if got := w.text(w.pool().Password()); got != secretOne {
		t.Error("the refusal changed the seat")
	}
}

// A failure after the new record is swapped in is reported, the new seat stays in place, the
// old record's credential stays in a claimed directory, and the next run, once the cause is
// gone, quarantines it and ends with nothing left over.
func TestPlaceReportsAFailureAfterTheSwapAndTheNextRunMendsIt(t *testing.T) {
	w := newWorld(t)
	w.mustPlace(passwordDoc(t, secretOne))
	w.writeFile(w.store.Released(), "not a directory")
	code, stderr, obj := w.run(passwordDoc(t, secretTwo))
	if code == 0 || strings.Contains(stderr, secretOne) || strings.Contains(stderr, secretTwo) {
		t.Errorf("exit %d, stderr %q", code, stderr)
	}
	if obj.Code != exits.CodeStore {
		t.Errorf("object %+v, want a store refusal", obj)
	}
	if got := w.text(w.pool().Password()); got != secretTwo {
		t.Error("the new seat is not in place")
	}
	w.lockIsFree()
	if err := os.Remove(w.store.Released()); err != nil {
		t.Fatal(err)
	}
	w.mustPlace(passwordDoc(t, secretTwo))
	if left := w.leftovers(); len(left) != 0 {
		t.Errorf("the next run left %v", left)
	}
	if got := w.holding(secretOne); len(got) != 0 {
		t.Errorf("the first credential is still in %v", got)
	}
}

func TestPlaceOfAReadErrorIsNotARefusalOfTheDocument(t *testing.T) {
	w := newWorld(t)
	boom := errors.New("the pipe broke")
	_, err := Place(t.Context(), w.deps, iotest.ErrReader(boom))
	if !errors.Is(err, boom) {
		t.Fatalf("err %v", err)
	}
	code, stderr, obj := reported(t, err)
	if code != exits.ExitInternal || obj.Code != exits.CodeInternal || !strings.Contains(stderr, "the pipe broke") {
		t.Errorf("exit %d object %+v stderr %q", code, obj, stderr)
	}
}

func TestPlaceWithoutWhatTheStoreNeedsIsAStoreRefusal(t *testing.T) {
	w := newWorld(t)
	w.deps.Reach = nil
	code, stderr, obj := w.run(passwordDoc(t, secretOne))
	if code != exits.ExitConfig || obj.Code != exits.CodeStore || !strings.Contains(stderr, "required") {
		t.Errorf("exit %d object %+v stderr %q", code, obj, stderr)
	}
}

func TestOSIsTheRealMachine(t *testing.T) {
	d := OS("/state", "/run", mustCheckout(t))
	if d.StateHome != "/state" || d.RuntimeDir != "/run" || d.Checkout != mustCheckout(t) {
		t.Errorf("deps %+v", d)
	}
	if d.Clock == nil || d.Reach == nil || d.Credential == nil {
		t.Fatalf("deps %+v lack a part", d)
	}
	if d.Credential.Kind() != seat.KindPassword {
		t.Errorf("the credential is a %s, want the pool password", d.Credential.Kind())
	}
	if d.Clock.Now().IsZero() {
		t.Error("the clock has no time")
	}
}
