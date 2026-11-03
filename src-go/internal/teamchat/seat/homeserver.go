package seat

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"

	"golang.org/x/sys/unix"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/matrix"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/proto"
)

// ErrHomeserverClash is a bundle for a team that the checkout has on another homeserver: another
// canonical base URL or another server name (Plan 00244 X8). Two homeserver hosts must not both
// declare one team id for the same checkout, since the pool and the session seat directories are
// keyed on the team id alone.
var ErrHomeserverClash = errors.New("seat: the team is on another homeserver in this checkout")

// serverNameRE is a server name as the checkout record may hold it: a lower-case DNS name.
var serverNameRE = regexp.MustCompile(`^[a-z0-9]([a-z0-9.-]*[a-z0-9])?$`)

const maxServerName = 253

// teamHome is the homeserver a checkout has one team's pool seats on: the team's server name and
// the canonical form of its member base URL (X9).
type teamHome struct {
	ServerName string `json:"server_name"`
	BaseURL    string `json:"base_url"`
}

// checkoutRecord is pool/<checkout>/checkout.json: the homeserver of each team the checkout has a
// pool seat of, written when the team's first bundle arrives (X8). Only the owner's store writes
// it, under the allocation lock.
type checkoutRecord struct {
	V     int                 `json:"v"`
	Teams map[string]teamHome `json:"teams"`
}

var checkoutKeys = []string{"v", "teams"}

// homeOf is the homeserver a bundle names.
func homeOf(b Bundle) (teamHome, error) {
	base, err := matrix.CanonicalMemberBase(b.BaseURL)
	if err != nil {
		return teamHome{}, invalid("seat.json: base_url is not a member URL")
	}
	return teamHome{ServerName: b.ServerName, BaseURL: base}, nil
}

func (h teamHome) check() error {
	if len(h.ServerName) > maxServerName || !serverNameRE.MatchString(h.ServerName) {
		return invalid("checkout.json: a server_name that is not a DNS name")
	}
	if canon, err := matrix.CanonicalMemberBase(h.BaseURL); err != nil || canon != h.BaseURL {
		return invalid("checkout.json: a base_url that is not a canonical member URL")
	}
	return nil
}

// decodeCheckoutRecord reads checkout.json strictly: exactly its keys, in every team's entry too,
// a team id for each key and a valid homeserver for each team.
func decodeCheckoutRecord(data []byte) (checkoutRecord, error) {
	var r checkoutRecord
	if err := decodeStrict(data, checkoutKeys, &r); err != nil {
		return checkoutRecord{}, err
	}
	if r.V != recordVersion {
		return checkoutRecord{}, invalid("checkout.json: v is not 1")
	}
	if r.Teams == nil {
		return checkoutRecord{}, invalid("checkout.json: teams is not an object")
	}
	if err := exactEntryKeys(data); err != nil {
		return checkoutRecord{}, err
	}
	for name, home := range r.Teams {
		if _, err := paths.ParseTeamID(name); err != nil {
			return checkoutRecord{}, invalid("checkout.json: a key that is not a team id")
		}
		if err := home.check(); err != nil {
			return checkoutRecord{}, err
		}
	}
	return r, nil
}

// exactEntryKeys is nil when every team's entry in data holds exactly the keys of a teamHome, in
// those spellings: encoding/json would match a capitalised key to a field, and take a missing one
// as empty.
func exactEntryKeys(data []byte) error {
	doc, err := proto.Parse(data)
	if err != nil {
		return fmt.Errorf("%w record: %w", ErrInvalid, err)
	}
	teams, found := doc.Get("teams")
	if !found || teams.Kind() != proto.KindObject {
		return invalid("checkout.json: teams is not an object")
	}
	for _, team := range teams.Members() {
		if team.Val.Kind() != proto.KindObject || len(team.Val.Members()) != 2 || !hasKey(team.Val, "server_name") || !hasKey(team.Val, "base_url") {
			return invalid("checkout.json: a team's entry is not a server_name and a base_url")
		}
	}
	return nil
}

func hasKey(v proto.Value, key string) bool {
	_, found := v.Get(key)
	return found
}

// readCheckout is the checkout record, an empty one when there is none yet.
func (s *Store) readCheckout() (checkoutRecord, error) {
	path := s.store.PoolCheckoutRecord(s.checkout)
	data, err := s.root.read(path, maxRecord)
	if errors.Is(err, unix.ENOENT) {
		return checkoutRecord{V: recordVersion, Teams: map[string]teamHome{}}, nil
	}
	if err != nil {
		return checkoutRecord{}, err
	}
	rec, err := decodeCheckoutRecord(data)
	if err != nil {
		return checkoutRecord{}, fmt.Errorf("%s: %w", path, err)
	}
	return rec, nil
}

// pin is a bundle judged against the checkout record: its team is recorded on the bundle's
// homeserver, or not recorded yet and written by write.
type pin struct {
	store    *Store
	rec      checkoutRecord
	team     string
	home     teamHome
	recorded bool
}

// pinOf judges b against the checkout record. A team recorded on another homeserver is
// ErrHomeserverClash naming both, and nothing is changed.
func (s *Store) pinOf(b Bundle) (pin, error) {
	home, err := homeOf(b)
	if err != nil {
		return pin{}, err
	}
	rec, err := s.readCheckout()
	if err != nil {
		return pin{}, err
	}
	p := pin{store: s, rec: rec, team: b.Team.String(), home: home}
	have, found := rec.Teams[p.team]
	if !found {
		return p, nil
	}
	if have != home {
		return pin{}, fmt.Errorf("%w: team %s is on %s (%s) here, and this seat.json names %s (%s)",
			ErrHomeserverClash, p.team, have.BaseURL, have.ServerName, home.BaseURL, home.ServerName)
	}
	p.recorded = true
	return p, nil
}

// write records the bundle's team on its homeserver, unless it is recorded already: the record is
// written afresh beside the old and renamed over it.
func (p pin) write() error {
	if p.recorded {
		return nil
	}
	p.rec.Teams[p.team] = p.home
	data, err := json.Marshal(p.rec)
	if err != nil {
		return err
	}
	if len(data) > maxRecord {
		return invalid("checkout.json: larger than its cap")
	}
	return p.store.root.replace(p.store.store.PoolCheckoutRecord(p.store.checkout), data)
}
