// Package fakehs is an in-memory homeserver that answers as Tuwunel does, for the tests of
// both sides of teamchat, the v2 agent bus (Plan 00209 SPEC-matrix.md M37-M43).
//
// It models what teamchat/matrix calls and nothing else: password login (as a new device or a
// named one, M48), logout, room account data, presence (M51), whoami, createRoom (room
// version 12, private_chat), invite, join (with a restricted join rule), leave, kick, send with
// transaction IDs per device, state get and put, the whole room state, redact, GET event, read
// receipts, /messages, /joined_rooms, /sync with an inline filter, long-polling and lazy-loaded
// members, and the Synapse-compatible admin API. Its answers are proven against the exchanges
// probe H4 recorded with a real Tuwunel (replay_test.go); where it says what Tuwunel says, the
// text is the recorded text. A request beyond the model is answered 500 and recorded, and
// CheckModelled reports it, rather than guessed at.
//
// Handle serves one request in process; Start serves the same over HTTP on 127.0.0.1.
package fakehs

import (
	"bytes"
	"context"
	"crypto/rand"
	"embed"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"
)

//go:embed static/*.json
var static embed.FS

const (
	roomVersion          = "12"
	defaultTimelineLimit = 10
	memberType           = "m.room.member"
	powerLevelsType      = "m.room.power_levels"
	joinRulesType        = "m.room.join_rules"
	createType           = "m.room.create"
	redactionType        = "m.room.redaction"
	membershipJoin       = "join"
	// membershipInvite is also the createRoom key and the power level of the same name.
	membershipInvite = "invite"
	membershipLeave  = "leave"
	membershipBan    = "ban"

	keyAdmin       = "admin"
	keyName        = "name"
	keyAccessToken = "access_token"
	keyDeviceID    = "device_id"
	keyUserID      = "user_id"
	keyEventID     = "event_id"
	keyDisplayName = "displayname"
	keyEvents      = "events"
	errUnknown     = "M_UNKNOWN"

	// tuwunelMaxRequestBytes is the max_request_size the installer renders (render.py:51).
	tuwunelMaxRequestBytes = 10_000_000
	// tuwunelLoginFailures is the failed-login burst the installer renders (render.py:58).
	tuwunelLoginFailures = 3
	// loginRetryAfterMS is the wait that burst refills at, 1/0.0033 per second (render.py:58).
	loginRetryAfterMS = 303_000
)

var localpartRE = regexp.MustCompile(`^[a-z0-9._=/+-]+$`)

// Options configures a Server.
type Options struct {
	// ServerName is the homeserver's name, the part of a user ID after the colon.
	ServerName string
	// Now is the clock for timestamps; nil is the wall clock.
	Now func() time.Time
	// Quirks are Tuwunel's behaviours beyond the specification.
	Quirks Quirks
}

// Quirks are Tuwunel's departures from the Matrix specification, each a switch (SPEC M40).
type Quirks struct {
	// MaxRequestBytes answers a body over it 413 M_TOO_LARGE; 0 is no limit.
	MaxRequestBytes int
	// AdminFalseIs500 answers a PUT v2/users carrying "admin": false for an account that is not
	// an admin with a 500, as H4 fixture 008 recorded.
	AdminFalseIs500 bool
	// LoginFailuresAllowed is how many wrong passwords an account may give before its password
	// logins are answered 429; 0 is no limit. Restart empties the count.
	LoginFailuresAllowed int
}

// TuwunelQuirks are the quirks of Tuwunel as the installer configures it.
func TuwunelQuirks() Quirks {
	return Quirks{
		MaxRequestBytes:      tuwunelMaxRequestBytes,
		AdminFalseIs500:      true,
		LoginFailuresAllowed: tuwunelLoginFailures,
	}
}

// Receipt is one read receipt the server accepted.
type Receipt struct {
	UserID  string
	RoomID  string
	Type    string
	EventID string
}

// Fault is a canned answer for the next matching requests (SPEC M41).
type Fault struct {
	// Method and Path select the requests: the method exactly, Path a substring of the
	// percent-encoded path.
	Method string
	Path   string
	// Status and Body are the answer; Header adds headers to it (Retry-After, say).
	Status int
	Body   []byte
	Header http.Header
	// Drop closes the connection before the request is handled: nothing happens.
	Drop bool
	// DropAfter handles the request, then closes the connection: the effect lands and the
	// answer is lost.
	DropAfter bool
	// Redirect answers with Location set to it, and Status (307 when 0).
	Redirect string
	// Times is how many requests the fault answers; 0 is one.
	Times int
}

type user struct {
	id          string
	password    string
	hasPassword bool
	admin       bool
	displayName string
	lastSeen    *int64
	deactivated bool
	locked      bool
}

type device struct {
	userID      string
	id          string
	displayName string
	lastSeen    *int64
}

type session struct {
	userID   string
	deviceID string
	token    string
}

type txnKey struct {
	userID, deviceID, roomID, txn string
}

type pendingFault struct {
	Fault

	remaining int
}

// Server is one homeserver's accounts, tokens and rooms. It is safe for concurrent use.
type Server struct {
	mu            sync.Mutex
	name          string
	now           func() time.Time
	quirks        Quirks
	regKey        string
	salt          []byte
	users         map[string]*user
	devices       []*device
	tokens        map[string]session
	rooms         map[string]*room
	roomOrder     []string
	txns          map[txnKey]string
	faults        []*pendingFault
	nonces        map[string]bool
	receipts      []Receipt
	accountData   map[accountDataKey]json.RawMessage
	presence      map[string]string
	unmodelled    []string
	loginFailures map[string]int
	pos           int
	ids           uint64
	changed       chan struct{}
	routes        []route
	web           webState
}

// New is an empty server.
func New(opts Options) *Server {
	now := opts.Now
	if now == nil {
		now = time.Now
	}
	s := &Server{
		name:          opts.ServerName,
		now:           now,
		quirks:        opts.Quirks,
		regKey:        hex.EncodeToString(randomBytes(32)),
		salt:          randomBytes(16),
		users:         map[string]*user{},
		tokens:        map[string]session{},
		rooms:         map[string]*room{},
		txns:          map[txnKey]string{},
		nonces:        map[string]bool{},
		accountData:   map[accountDataKey]json.RawMessage{},
		presence:      map[string]string{},
		loginFailures: map[string]int{},
		changed:       make(chan struct{}),
	}
	s.routes = s.routeTable()
	return s
}

// RegistrationKey is the shared secret of the admin registration endpoint.
func (s *Server) RegistrationKey() string { return s.regKey }

// SetQuirks replaces the quirks.
func (s *Server) SetQuirks(q Quirks) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.quirks = q
}

// AddUser seeds an account without the admin flow; an empty password is one no login matches.
// It panics on a localpart the server would refuse, a fault in the calling test.
func (s *Server) AddUser(localpart, password string, admin bool) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	u, err := s.createUser(localpart, password, password != "", admin, "")
	if err != nil {
		panic(err)
	}
	return u.id
}

// MintToken gives a new access token, on a new device, for an existing account.
func (s *Server) MintToken(userID string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.users[userID]; !ok {
		return "", fmt.Errorf("fakehs: no account %s", userID)
	}
	return s.newToken(userID, "").token, nil
}

// Inject queues f for the next matching requests.
func (s *Server) Inject(f Fault) {
	s.mu.Lock()
	defer s.mu.Unlock()
	times := f.Times
	if times <= 0 {
		times = 1
	}
	s.faults = append(s.faults, &pendingFault{Fault: f, remaining: times})
}

// Receipts are the read receipts accepted so far, in order.
func (s *Server) Receipts() []Receipt {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]Receipt(nil), s.receipts...)
}

// CheckModelled reports the first request the model did not cover, if any.
func (s *Server) CheckModelled() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.unmodelled) == 0 {
		return nil
	}
	return fmt.Errorf("fake: unmodelled: %s", s.unmodelled[0])
}

// Handle serves one request: path as on the wire (percent-encoded), token empty for none. It
// gives the status and the JSON answer. A fault that drops the connection is answered with
// status 0 and no body here; over HTTP the connection is closed instead.
func (s *Server) Handle(ctx context.Context, method, path string, query url.Values, body []byte, token string) (int, []byte) {
	out := s.process(ctx, method, path, query, body, token)
	return out.status, out.body
}

func randomBytes(n int) []byte {
	b := make([]byte, n)
	// crypto/rand.Read does not fail on Linux; it is documented to crash the program instead.
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return b
}

// apiError is an answer the server gives: a status, an errcode and its text, and the wait a
// 429 names.
type apiError struct {
	status       int
	errcode      string
	text         string
	retryAfterMS int
}

func (e *apiError) Error() string { return fmt.Sprintf("%d %s: %s", e.status, e.errcode, e.text) }

func forbidden(text string) error {
	return &apiError{status: http.StatusForbidden, errcode: "M_FORBIDDEN", text: text}
}

func notFound(text string) error {
	return &apiError{status: http.StatusNotFound, errcode: "M_NOT_FOUND", text: text}
}

func invalidParam(text string) error {
	return &apiError{status: http.StatusBadRequest, errcode: "M_INVALID_PARAM", text: text}
}

func notJSON() error {
	return &apiError{status: http.StatusBadRequest, errcode: "M_NOT_JSON", text: "Content not JSON."}
}

// unmodelledError is a request that needs behaviour H4 did not record and the fake does not
// model.
type unmodelledError string

func (e unmodelledError) Error() string { return string(e) }

func unmodelled(format string, args ...any) error {
	return unmodelledError(fmt.Sprintf(format, args...))
}

// request is one decoded request as a handler sees it; done closes when its caller goes.
type request struct {
	done     <-chan struct{}
	userID   string
	deviceID string
	query    url.Values
	body     any
}

func (r *request) obj() (map[string]any, error) {
	body, ok := r.body.(map[string]any)
	if !ok {
		return nil, notJSON()
	}
	return body, nil
}

type authKind int

const (
	authNone authKind = iota
	authUser
	authAdmin
)

type handler func(r *request, args []string) (any, error)

type route struct {
	method  string
	pattern *regexp.Regexp
	handle  handler
	auth    authKind
}

const (
	roomPrefix         = `/_matrix/client/v3/rooms/([^/]+)`
	accountDataPattern = `/_matrix/client/v3/user/([^/]+)/rooms/([^/]+)/account_data/([^/]+)`
)

func (s *Server) routeTable() []route {
	table := []struct {
		method, pattern string
		handle          handler
		auth            authKind
	}{
		{http.MethodGet, `/_matrix/client/versions`, staticAnswer("client-versions.json"), authNone},
		{http.MethodGet, `/_tuwunel/server_version`, staticAnswer("server-version.json"), authNone},
		{http.MethodGet, `/_matrix/client/v3/login`, staticAnswer("login-flows.json"), authNone},
		{http.MethodPost, `/_matrix/client/v3/login`, s.login, authNone},
		{http.MethodPost, `/_matrix/client/v3/logout`, s.logout, authUser},
		{http.MethodGet, accountDataPattern, s.getAccountData, authUser},
		{http.MethodPut, accountDataPattern, s.putAccountData, authUser},
		{http.MethodPut, `/_matrix/client/v3/presence/([^/]+)/status`, s.putPresence, authUser},
		{http.MethodGet, `/_matrix/client/v3/account/whoami`, whoami, authUser},
		{http.MethodPost, `/_matrix/client/v1/login/get_token`, getToken, authUser},
		{http.MethodGet, `/_matrix/client/v3/joined_rooms`, s.joinedRooms, authUser},
		{http.MethodPost, `/_matrix/client/v3/createRoom`, s.createRoom, authUser},
		{http.MethodPost, roomPrefix + `/invite`, s.invite, authUser},
		{http.MethodPost, roomPrefix + `/join`, s.join, authUser},
		{http.MethodPost, roomPrefix + `/leave`, s.leave, authUser},
		{http.MethodPost, roomPrefix + `/kick`, s.kick, authUser},
		{http.MethodPut, roomPrefix + `/send/([^/]+)/([^/]+)`, s.send, authUser},
		{http.MethodPut, roomPrefix + `/state/([^/]+)(?:/([^/]*))?`, s.putState, authUser},
		{http.MethodGet, roomPrefix + `/state/([^/]+)(?:/([^/]*))?`, s.getState, authUser},
		{http.MethodGet, roomPrefix + `/state`, s.fullState, authUser},
		{http.MethodPut, roomPrefix + `/redact/([^/]+)/([^/]+)`, s.redact, authUser},
		{http.MethodGet, roomPrefix + `/event/([^/]+)`, s.getEvent, authUser},
		{http.MethodPost, roomPrefix + `/receipt/([^/]+)/([^/]+)`, s.receipt, authUser},
		{http.MethodGet, roomPrefix + `/messages`, s.messages, authUser},
		{http.MethodGet, `/_matrix/client/v3/sync`, s.sync, authUser},
		{http.MethodGet, adminPrefix + `/v1/register`, s.registerNonce, authNone},
		{http.MethodPost, adminPrefix + `/v1/register`, s.register, authNone},
		{http.MethodGet, adminPrefix + `/v2/users`, s.listUsers, authAdmin},
		{http.MethodGet, adminPrefix + `/v2/users/([^/]+)`, s.getUser, authAdmin},
		{http.MethodPut, adminPrefix + `/v2/users/([^/]+)`, s.putUser, authAdmin},
		{http.MethodGet, adminPrefix + `/v2/users/([^/]+)/devices`, s.listDevices, authAdmin},
		{http.MethodPost, adminPrefix + `/v1/users/([^/]+)/login`, s.mint, authAdmin},
		{http.MethodPost, adminPrefix + `/v1/reset_password/([^/]+)`, s.resetPassword, authAdmin},
	}
	out := make([]route, 0, len(table))
	for _, row := range table {
		out = append(out, route{row.method, regexp.MustCompile(`^` + row.pattern + `$`), row.handle, row.auth})
	}
	return out
}

func staticAnswer(name string) handler {
	return func(*request, []string) (any, error) {
		raw, err := static.ReadFile("static/" + name)
		if err != nil {
			return nil, fmt.Errorf("fakehs: the embedded answer %s: %w", name, err)
		}
		return json.RawMessage(raw), nil
	}
}

// outcome is what one request came to, before HTTP writes it.
type outcome struct {
	status int
	body   []byte
	fault  *Fault
}

func (s *Server) process(ctx context.Context, method, path string, query url.Values, body []byte, token string) outcome {
	s.mu.Lock()
	defer s.mu.Unlock()
	f := s.takeFault(method, path)
	switch {
	case f == nil:
		return s.answer(ctx, method, path, query, body, token)
	case f.Drop:
		return outcome{fault: f}
	case f.DropAfter:
		out := s.answer(ctx, method, path, query, body, token)
		out.fault = f
		return out
	default:
		return outcome{status: f.Status, body: append([]byte(nil), f.Body...), fault: f}
	}
}

func (s *Server) takeFault(method, path string) *Fault {
	for _, f := range s.faults {
		if f.remaining > 0 && f.Method == method && strings.Contains(path, f.Path) {
			f.remaining--
			fault := f.Fault
			return &fault
		}
	}
	return nil
}

func (s *Server) answer(ctx context.Context, method, path string, query url.Values, body []byte, token string) outcome {
	value, err := s.dispatch(ctx, method, path, query, body, token)
	if err == nil {
		raw, merr := json.Marshal(value)
		if merr == nil {
			return outcome{status: http.StatusOK, body: raw}
		}
		err = merr
	}
	var api *apiError
	var gap unmodelledError
	switch {
	case errors.As(err, &api):
		return errorOutcome(api)
	case errors.As(err, &gap):
		s.unmodelled = append(s.unmodelled, string(gap))
		return errorOutcome(&apiError{
			status: http.StatusInternalServerError, errcode: errUnknown, text: "fake: unmodelled: " + string(gap),
		})
	default:
		return errorOutcome(&apiError{status: http.StatusInternalServerError, errcode: errUnknown, text: "fake: " + err.Error()})
	}
}

func errorOutcome(e *apiError) outcome {
	answer := map[string]any{"errcode": e.errcode, "error": e.text}
	if e.retryAfterMS > 0 {
		answer["retry_after_ms"] = e.retryAfterMS
	}
	raw, err := json.Marshal(answer)
	if err != nil {
		panic(err) // strings and an int always encode
	}
	return outcome{status: e.status, body: raw}
}

// decodeBody is a request body as JSON, numbers kept as written; an empty body is nil.
func decodeBody(body []byte) (any, error) {
	var v any
	if len(body) == 0 {
		return v, nil
	}
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.UseNumber()
	if err := dec.Decode(&v); err != nil || dec.More() {
		return nil, notJSON()
	}
	return v, nil
}

func (s *Server) dispatch(ctx context.Context, method, path string, query url.Values, body []byte, token string) (any, error) {
	if s.quirks.MaxRequestBytes > 0 && len(body) > s.quirks.MaxRequestBytes {
		return nil, &apiError{status: http.StatusRequestEntityTooLarge, errcode: "M_TOO_LARGE", text: "Request body too large."}
	}
	for _, rt := range s.routes {
		match := rt.pattern.FindStringSubmatch(path)
		if rt.method != method || match == nil {
			continue
		}
		decoded, err := decodeBody(body)
		if err != nil {
			return nil, err
		}
		r := &request{done: ctx.Done(), query: query, body: decoded}
		if query == nil {
			r.query = url.Values{}
		}
		if rt.auth != authNone {
			if err := s.authenticate(r, token, rt.auth); err != nil {
				return nil, err
			}
		}
		args := make([]string, 0, len(match)-1)
		for _, group := range match[1:] {
			args = append(args, unquote(group))
		}
		return rt.handle(r, args)
	}
	return nil, &apiError{status: http.StatusNotFound, errcode: "M_UNRECOGNIZED", text: "Unrecognized request"}
}

// unquote decodes %XX escapes and leaves anything else as it is, as Python's
// urllib.parse.unquote does for the paths v1's fake routed.
func unquote(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '%' && i+2 < len(s) {
			if v, err := hex.DecodeString(s[i+1 : i+3]); err == nil {
				b.WriteByte(v[0])
				i += 2
				continue
			}
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

func (s *Server) authenticate(r *request, token string, kind authKind) error {
	if token == "" {
		return &apiError{status: http.StatusUnauthorized, errcode: "M_MISSING_TOKEN", text: "Missing access token."}
	}
	sess, ok := s.tokens[token]
	if !ok {
		return &apiError{status: http.StatusUnauthorized, errcode: "M_UNKNOWN_TOKEN", text: "M_UNKNOWN_TOKEN: Unknown access token."}
	}
	u := s.users[sess.userID]
	// H4 recorded no request by a locked account, so what Tuwunel answers is not guessed.
	if u.locked {
		return unmodelled("a request by the locked account %s", sess.userID)
	}
	seen := s.nowMS()
	u.lastSeen = &seen
	if d := s.device(sess.userID, sess.deviceID); d != nil {
		d.lastSeen = &seen
	}
	if kind == authAdmin && !u.admin {
		return forbidden("M_FORBIDDEN: Only server administrators can use this endpoint")
	}
	r.userID, r.deviceID = sess.userID, sess.deviceID
	return nil
}

func (s *Server) nowMS() int64 { return s.now().UnixMilli() }

func (s *Server) userIDOf(localpart string) string { return "@" + localpart + ":" + s.name }

func (s *Server) createUser(localpart, password string, hasPassword, admin bool, displayName string) (*user, error) {
	if !localpartRE.MatchString(localpart) {
		return nil, &apiError{status: http.StatusBadRequest, errcode: "M_INVALID_USERNAME", text: "User ID is not valid."}
	}
	id := s.userIDOf(localpart)
	if _, taken := s.users[id]; taken {
		return nil, &apiError{status: http.StatusBadRequest, errcode: "M_USER_IN_USE", text: "User ID already taken."}
	}
	if displayName == "" {
		displayName = localpart
	}
	u := &user{id: id, password: password, hasPassword: hasPassword, admin: admin, displayName: displayName}
	s.users[id] = u
	return u, nil
}

// newToken signs userID in on a new device.
func (s *Server) newToken(userID, displayName string) session {
	sess := session{
		userID:   userID,
		deviceID: strings.ToUpper(hex.EncodeToString(randomBytes(5))),
		token:    newTokenText(),
	}
	s.tokens[sess.token] = sess
	s.devices = append(s.devices, &device{userID: userID, id: sess.deviceID, displayName: displayName})
	return sess
}

func newTokenText() string { return base64.RawURLEncoding.EncodeToString(randomBytes(24)) }

// signedIn is the answer of a login or a registration.
func (s *Server) signedIn(sess session) map[string]any {
	return map[string]any{keyAccessToken: sess.token, keyDeviceID: sess.deviceID, "home_server": s.name, keyUserID: sess.userID}
}

func (s *Server) device(userID, deviceID string) *device {
	for _, d := range s.devices {
		if d.userID == userID && d.id == deviceID {
			return d
		}
	}
	return nil
}

// logoutAll revokes every token of the account and deletes its devices.
func (s *Server) logoutAll(userID string) {
	for token, sess := range s.tokens {
		if sess.userID == userID {
			delete(s.tokens, token)
		}
	}
	kept := s.devices[:0]
	for _, d := range s.devices {
		if d.userID != userID {
			kept = append(kept, d)
		}
	}
	s.devices = kept
}

func (s *Server) login(r *request, _ []string) (any, error) {
	body, err := r.obj()
	if err != nil {
		return nil, err
	}
	if body["type"] != "m.login.password" {
		return nil, unmodelled("login type %v", body["type"])
	}
	name := stringAt(body, "user")
	if ident, ok := body["identifier"].(map[string]any); ok {
		name = stringAt(ident, "user")
	}
	if !strings.HasPrefix(name, "@") {
		name = s.userIDOf(name)
	}
	if s.quirks.LoginFailuresAllowed > 0 && s.loginFailures[name] >= s.quirks.LoginFailuresAllowed {
		return nil, &apiError{
			status: http.StatusTooManyRequests, errcode: "M_LIMIT_EXCEEDED", text: "Too many requests.",
			retryAfterMS: loginRetryAfterMS,
		}
	}
	u := s.users[name]
	password, isText := body["password"].(string)
	if u == nil || !u.hasPassword || !isText || u.password != password {
		s.loginFailures[name]++
		return nil, forbidden("Wrong username or password.")
	}
	// H4 recorded no sign-in to a deactivated account, so what Tuwunel answers is not guessed.
	if u.deactivated {
		return nil, unmodelled("signing in to the deactivated account %s", u.id)
	}
	deviceID, err := deviceIDOf(body)
	if err != nil {
		return nil, err
	}
	return s.signedIn(s.signIn(u.id, deviceID, stringAt(body, "initial_device_display_name"))), nil
}

func whoami(r *request, _ []string) (any, error) {
	return map[string]any{keyDeviceID: r.deviceID, keyUserID: r.userID}, nil
}

func getToken(*request, []string) (any, error) {
	return nil, forbidden("M_FORBIDDEN: Login via an existing session is not enabled")
}

func stringAt(obj map[string]any, key string) string {
	text, ok := obj[key].(string)
	if !ok {
		return ""
	}
	return text
}
