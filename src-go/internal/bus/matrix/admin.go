package matrix

import (
	"context"
	"crypto/hmac"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const adminPrefix = "/_synapse/admin"

// Admin is the host side's client: every client-server call, and the Synapse-compatible admin
// API Tuwunel serves (SPEC M27-M32). It reaches only http://127.0.0.1:<port>.
type Admin struct {
	*Client
}

// NewAdmin builds an admin-side client; the token may be empty until Register or Login gives
// one.
func NewAdmin(cfg Config) (*Admin, error) {
	if len(cfg.PlainHTTPHosts) > 0 {
		return nil, invalid("an admin client takes no plain_http_hosts")
	}
	base, err := adminBase(cfg.BaseURL)
	if err != nil {
		return nil, err
	}
	c, err := newClient(cfg, base, adminAgent, adminLimit)
	if err != nil {
		return nil, err
	}
	return &Admin{Client: c}, nil
}

// WithToken is an admin client like a that sends token.
func (a *Admin) WithToken(token string) (*Admin, error) {
	c, err := a.Client.WithToken(token)
	if err != nil {
		return nil, err
	}
	return &Admin{Client: c}, nil
}

// RegistrationMAC is Synapse's shared-secret registration MAC: HMAC-SHA1 under the secret over
// nonce, user, password and "admin" or "notadmin", NUL-separated, in hex.
func RegistrationMAC(secret, nonce, userName, password string, admin bool) string {
	kind := "notadmin"
	if admin {
		kind = "admin"
	}
	mac := hmac.New(sha1.New, []byte(secret))
	mac.Write([]byte(strings.Join([]string{nonce, userName, password, kind}, "\x00")))
	return hex.EncodeToString(mac.Sum(nil))
}

// Registration is an account made through the shared secret.
type Registration struct {
	Username    string
	Password    string
	DisplayName string
	Admin       bool
}

// Register makes an account through the shared-secret registration endpoint (SPEC M27): it
// asks a single-use nonce, then posts the account with its MAC. Neither call sends a token.
func (a *Admin) Register(ctx context.Context, sharedSecret string, reg Registration) (Session, error) {
	path := adminPrefix + "/v1/register"
	nonceCall := call{method: http.MethodGet, path: path, noAuth: true}
	var out struct {
		Nonce string `json:"nonce"`
	}
	if err := a.object(ctx, nonceCall, &out); err != nil {
		return Session{}, err
	}
	if !isToken(out.Nonce) {
		return Session{}, nonceCall.malformed()
	}
	body := map[string]any{
		"nonce": out.Nonce, "username": reg.Username, keyPassword: reg.Password, "admin": reg.Admin,
		"mac": RegistrationMAC(sharedSecret, out.Nonce, reg.Username, reg.Password, reg.Admin),
	}
	if reg.DisplayName != "" {
		body["displayname"] = reg.DisplayName
	}
	return a.session(ctx, call{method: http.MethodPost, path: path, body: jsonBody(body), noAuth: true})
}

// Account is a user account as the admin API shows it.
type Account struct {
	UserID      string
	DisplayName string
	Admin       bool
	Deactivated bool
	Locked      bool
}

func userPath(userID string) (string, error) {
	return pathf(adminPrefix+"/v2/users/%s", userID)
}

type wireAccount struct {
	Name        string  `json:"name"`
	DisplayName *string `json:"displayname"`
	Admin       bool    `json:"admin"`
	Deactivated bool    `json:"deactivated"`
	Locked      bool    `json:"locked"`
}

func (w wireAccount) account() Account {
	out := Account{UserID: w.Name, Admin: w.Admin, Deactivated: w.Deactivated, Locked: w.Locked}
	if w.DisplayName != nil {
		out.DisplayName = *w.DisplayName
	}
	return out
}

// Account is the account; found is false when there is none (SPEC M29).
func (a *Admin) Account(ctx context.Context, userID string) (Account, bool, error) {
	path, err := userPath(userID)
	if err != nil {
		return Account{}, false, err
	}
	cl := call{method: http.MethodGet, path: path}
	var out wireAccount
	err = a.object(ctx, cl, &out)
	switch {
	case errors.Is(err, ErrNotFound):
		return Account{}, false, nil
	case err != nil:
		return Account{}, false, err
	case out.Name != userID:
		return Account{}, false, cl.malformed()
	}
	return out.account(), true, nil
}

// CreateAccount makes a plain account with a password. The body has no admin key (Tuwunel
// answers an "admin": false for a non-admin with a 500), and the answer must be the account
// asked for and not an admin.
func (a *Admin) CreateAccount(ctx context.Context, userID, password, displayName string) (Account, error) {
	out, cl, err := a.putUser(ctx, userID, map[string]any{"password": password, "displayname": displayName})
	if err != nil {
		return Account{}, err
	}
	if out.Name != userID || out.Admin {
		return Account{}, cl.fail(ErrRefused, http.StatusOK, "", "the answer is not the plain account asked for", false)
	}
	return out.account(), nil
}

// ResetPassword sets a new password and signs every device of the account out, the only
// revocation Tuwunel offers.
func (a *Admin) ResetPassword(ctx context.Context, userID, password string) error {
	_, _, err := a.putUser(ctx, userID, map[string]any{"password": password, "logout_devices": true})
	return err
}

// SetDeactivated deactivates (or, where the server allows it, reactivates) an account.
func (a *Admin) SetDeactivated(ctx context.Context, userID string, deactivated bool) error {
	_, _, err := a.putUser(ctx, userID, map[string]any{"deactivated": deactivated})
	return err
}

// SetLocked locks or unlocks an account.
func (a *Admin) SetLocked(ctx context.Context, userID string, locked bool) error {
	_, _, err := a.putUser(ctx, userID, map[string]any{"locked": locked})
	return err
}

// MintToken gives a new access token, on a new device, for an account (SPEC M30).
func (a *Admin) MintToken(ctx context.Context, userID string) (Token, error) {
	path, err := pathf(adminPrefix+"/v1/users/%s/login", userID)
	if err != nil {
		return "", err
	}
	cl := call{method: http.MethodPost, path: path, body: []byte(`{}`)}
	var out struct {
		AccessToken string `json:"access_token"`
	}
	if err := a.object(ctx, cl, &out); err != nil {
		return "", err
	}
	if !isToken(out.AccessToken) {
		return "", cl.malformed()
	}
	return Token(out.AccessToken), nil
}

// ServerAdmins are the user IDs of the server's admin accounts (SPEC M31). A list that would
// need a second page is malformed: the host's server has a handful of accounts.
func (a *Admin) ServerAdmins(ctx context.Context) ([]string, error) {
	cl := call{method: http.MethodGet, path: adminPrefix + "/v2/users", query: url.Values{"admins": {"true"}}}
	var out struct {
		Users *[]struct {
			Name string `json:"name"`
		} `json:"users"`
		NextToken json.RawMessage `json:"next_token"`
	}
	if err := a.object(ctx, cl, &out); err != nil {
		return nil, err
	}
	if out.Users == nil || (out.NextToken != nil && string(out.NextToken) != "null") {
		return nil, cl.malformed()
	}
	names := make([]string, 0, len(*out.Users))
	for _, u := range *out.Users {
		if u.Name == "" {
			return nil, cl.malformed()
		}
		names = append(names, u.Name)
	}
	return names, nil
}

// Device is one of an account's devices; LastSeen is zero when the server has none.
type Device struct {
	ID       string
	LastSeen time.Time
}

// Devices are an account's devices (SPEC M32).
func (a *Admin) Devices(ctx context.Context, userID string) ([]Device, error) {
	path, err := pathf(adminPrefix+"/v2/users/%s/devices", userID)
	if err != nil {
		return nil, err
	}
	cl := call{method: http.MethodGet, path: path}
	var out struct {
		Devices *[]struct {
			DeviceID   string `json:"device_id"`
			LastSeenTS *int64 `json:"last_seen_ts"`
		} `json:"devices"`
	}
	if err := a.object(ctx, cl, &out); err != nil {
		return nil, err
	}
	if out.Devices == nil {
		return nil, cl.malformed()
	}
	devices := make([]Device, 0, len(*out.Devices))
	for _, d := range *out.Devices {
		if d.DeviceID == "" {
			return nil, cl.malformed()
		}
		dev := Device{ID: d.DeviceID}
		if d.LastSeenTS != nil {
			dev.LastSeen = time.UnixMilli(*d.LastSeenTS).UTC()
		}
		devices = append(devices, dev)
	}
	return devices, nil
}

func (a *Admin) putUser(ctx context.Context, userID string, body map[string]any) (wireAccount, call, error) {
	var out wireAccount
	path, err := userPath(userID)
	if err != nil {
		return out, call{}, err
	}
	cl := call{method: http.MethodPut, path: path, body: jsonBody(body)}
	return out, cl, a.object(ctx, cl, &out)
}
