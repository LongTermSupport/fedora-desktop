package matrix

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"time"
)

const (
	sendRetryWait = time.Second
	keyPassword   = "password"
)

var (
	txnRE     = regexp.MustCompile(`^[A-Za-z0-9._~-]{1,64}$`)
	eventIDRE = regexp.MustCompile(`^\$[\x21-\x7e]{1,254}$`)
	roomIDRE  = regexp.MustCompile(`^![\x21-\x7e]{1,254}$`)
)

// Identity is who a token belongs to.
type Identity struct {
	UserID   string
	DeviceID string
}

// Versions are the specification versions the server supports (SPEC M18); asked without the
// token.
func (c *Client) Versions(ctx context.Context) ([]string, error) {
	cl := call{method: http.MethodGet, path: "/_matrix/client/versions", noAuth: true}
	var out struct {
		Versions *[]string `json:"versions"`
	}
	if err := c.object(ctx, cl, &out); err != nil {
		return nil, err
	}
	if out.Versions == nil {
		return nil, cl.malformed()
	}
	return *out.Versions, nil
}

// Whoami is the user and device the token belongs to (SPEC M19).
func (c *Client) Whoami(ctx context.Context) (Identity, error) {
	cl := call{method: http.MethodGet, path: clientPrefix + "/account/whoami"}
	var out struct {
		UserID   string `json:"user_id"`
		DeviceID string `json:"device_id"`
	}
	if err := c.object(ctx, cl, &out); err != nil {
		return Identity{}, err
	}
	if out.UserID == "" {
		return Identity{}, cl.malformed()
	}
	return Identity{UserID: out.UserID, DeviceID: out.DeviceID}, nil
}

// Login signs in with a password as a new device (SPEC M28); it sends no token.
func (c *Client) Login(ctx context.Context, userName, password, deviceName string) (Session, error) {
	body := passwordLogin(userName, password)
	body["initial_device_display_name"] = deviceName
	return c.session(ctx, loginCall(body))
}

// passwordLogin is the body of an m.login.password login as userName.
func passwordLogin(userName, password string) map[string]any {
	return map[string]any{
		"type":       "m.login.password",
		"identifier": map[string]string{"type": "m.id.user", "user": userName},
		keyPassword:  password,
	}
}

func loginCall(body map[string]any) call {
	return call{method: http.MethodPost, path: clientPrefix + "/login", body: jsonBody(body), noAuth: true}
}

// session reads an answer that signs a device in.
func (c *Client) session(ctx context.Context, cl call) (Session, error) {
	var out struct {
		UserID      string `json:"user_id"`
		AccessToken string `json:"access_token"`
		DeviceID    string `json:"device_id"`
	}
	if err := c.object(ctx, cl, &out); err != nil {
		return Session{}, err
	}
	if out.UserID == "" || !isToken(out.AccessToken) {
		return Session{}, cl.malformed()
	}
	return Session{UserID: out.UserID, AccessToken: Token(out.AccessToken), DeviceID: out.DeviceID}, nil
}

// NewTxnID is a fresh transaction ID: "pb" and 32 hex digits from crypto/rand.
func NewTxnID() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return "pb" + hex.EncodeToString(b), nil
}

// Send sends an event (SPEC M22). An empty txnID is made fresh; a caller that must survive its
// own crash passes one it stored first. An answer that was lost is retried with the same
// transaction ID, 3 tries in all, 1 s apart, so the server stores the event once.
func (c *Client) Send(ctx context.Context, roomID, eventType, txnID string, content json.RawMessage) (string, error) {
	txnID, err := transactionID(txnID)
	if err != nil {
		return "", err
	}
	if err = rawObject(content); err != nil {
		return "", err
	}
	path, err := pathf(clientPrefix+"/rooms/%s/send/%s/%s", roomID, eventType, txnID)
	if err != nil {
		return "", err
	}
	return c.putOnce(ctx, call{method: http.MethodPut, path: path, body: content})
}

// Redact redacts one event, with the reason when one is given (SPEC M49). The transaction ID is
// made and checked as Send's is, but a lost answer is not retried: whether Tuwunel stores a
// replayed redact once is not known, so the caller reads whether the event is redacted instead.
func (c *Client) Redact(ctx context.Context, roomID, eventID, txnID, reason string) (string, error) {
	txnID, err := transactionID(txnID)
	if err != nil {
		return "", err
	}
	path, err := pathf(clientPrefix+"/rooms/%s/redact/%s/%s", roomID, eventID, txnID)
	if err != nil {
		return "", err
	}
	body := map[string]string{}
	if reason != "" {
		body["reason"] = reason
	}
	return c.eventID(ctx, call{method: http.MethodPut, path: path, body: jsonBody(body)})
}

// transactionID is txnID checked, or a fresh one when it is empty.
func transactionID(txnID string) (string, error) {
	if txnID == "" {
		fresh, err := NewTxnID()
		if err != nil {
			return "", invalid("no transaction ID could be made")
		}
		txnID = fresh
	}
	if !txnRE.MatchString(txnID) {
		return "", invalid("a transaction ID is 1 to 64 of [A-Za-z0-9._~-]")
	}
	return txnID, nil
}

// putOnce sends a PUT under a transaction ID, retrying a lost answer with the same ID, 3 tries
// in all, 1 s apart, so the server stores the event once.
func (c *Client) putOnce(ctx context.Context, cl call) (string, error) {
	for try := 1; ; try++ {
		id, err := c.eventID(ctx, cl)
		var e *Error
		if err == nil || try == maxTries || !errors.As(err, &e) || !e.answerLost() {
			return id, err
		}
		if err := c.clock.Sleep(ctx, sendRetryWait); err != nil {
			return "", cl.cancelled(ctx.Err())
		}
	}
}

// answerLost is a request whose answer never came whole: the effect may have landed.
func (e *Error) answerLost() bool { return errors.Is(e, ErrUnreachable) && e.transient }

// eventID sends a call answered with the event it made.
func (c *Client) eventID(ctx context.Context, cl call) (string, error) {
	var out struct {
		EventID string `json:"event_id"`
	}
	if err := c.object(ctx, cl, &out); err != nil {
		return "", err
	}
	if !eventIDRE.MatchString(out.EventID) {
		return "", cl.malformed()
	}
	return out.EventID, nil
}

// PutState sets a state event (SPEC M24); the state key may be empty.
func (c *Client) PutState(ctx context.Context, roomID, eventType, stateKey string, content json.RawMessage) (string, error) {
	if err := rawObject(content); err != nil {
		return "", err
	}
	path, err := statePath(roomID, eventType, stateKey)
	if err != nil {
		return "", err
	}
	return c.eventID(ctx, call{method: http.MethodPut, path: path, body: content})
}

func statePath(roomID, eventType, stateKey string) (string, error) {
	return pathKey(clientPrefix+"/rooms/%s/state/%s/%s", true, roomID, eventType, stateKey)
}

// GetState is a state event's content.
func (c *Client) GetState(ctx context.Context, roomID, eventType, stateKey string) (json.RawMessage, error) {
	path, err := statePath(roomID, eventType, stateKey)
	if err != nil {
		return nil, err
	}
	var out json.RawMessage
	return out, c.object(ctx, call{method: http.MethodGet, path: path}, &out)
}

// GetStateEvent is the whole state event, its sender included.
func (c *Client) GetStateEvent(ctx context.Context, roomID, eventType, stateKey string) (json.RawMessage, error) {
	path, err := statePath(roomID, eventType, stateKey)
	if err != nil {
		return nil, err
	}
	var out json.RawMessage
	cl := call{method: http.MethodGet, path: path, query: url.Values{"format": {"event"}}}
	return out, c.object(ctx, cl, &out)
}

// RoomState is every current state event of the room.
func (c *Client) RoomState(ctx context.Context, roomID string) ([]json.RawMessage, error) {
	path, err := pathf(clientPrefix+"/rooms/%s/state", roomID)
	if err != nil {
		return nil, err
	}
	cl := call{method: http.MethodGet, path: path}
	var out []json.RawMessage
	if err := c.array(ctx, cl, &out); err != nil {
		return nil, err
	}
	if !allObjects(out) {
		return nil, cl.malformed()
	}
	return out, nil
}

func allObjects(events []json.RawMessage) bool {
	for _, e := range events {
		if first(e) != '{' {
			return false
		}
	}
	return true
}

// GetEvent is one event of the room.
func (c *Client) GetEvent(ctx context.Context, roomID, eventID string) (json.RawMessage, error) {
	path, err := pathf(clientPrefix+"/rooms/%s/event/%s", roomID, eventID)
	if err != nil {
		return nil, err
	}
	var out json.RawMessage
	return out, c.object(ctx, call{method: http.MethodGet, path: path}, &out)
}

// Join joins a room and gives its ID.
func (c *Client) Join(ctx context.Context, roomID string) (string, error) {
	path, err := pathf(clientPrefix+"/rooms/%s/join", roomID)
	if err != nil {
		return "", err
	}
	return c.roomID(ctx, call{method: http.MethodPost, path: path, body: []byte(`{}`)})
}

func (c *Client) roomID(ctx context.Context, cl call) (string, error) {
	var out struct {
		RoomID string `json:"room_id"`
	}
	if err := c.object(ctx, cl, &out); err != nil {
		return "", err
	}
	if !roomIDRE.MatchString(out.RoomID) {
		return "", cl.malformed()
	}
	return out.RoomID, nil
}

// Leave leaves a room.
func (c *Client) Leave(ctx context.Context, roomID string) error {
	path, err := pathf(clientPrefix+"/rooms/%s/leave", roomID)
	if err != nil {
		return err
	}
	return c.empty(ctx, call{method: http.MethodPost, path: path, body: []byte(`{}`)})
}

// empty sends a call whose answer carries nothing the caller needs.
func (c *Client) empty(ctx context.Context, cl call) error {
	var out map[string]json.RawMessage
	return c.object(ctx, cl, &out)
}

// ReadReceipt marks an event read.
func (c *Client) ReadReceipt(ctx context.Context, roomID, eventID string) error {
	path, err := pathf(clientPrefix+"/rooms/%s/receipt/m.read/%s", roomID, eventID)
	if err != nil {
		return err
	}
	return c.empty(ctx, call{method: http.MethodPost, path: path, body: []byte(`{}`)})
}

// Invite invites a user.
func (c *Client) Invite(ctx context.Context, roomID, userID string) error {
	path, err := pathf(clientPrefix+"/rooms/%s/invite", roomID)
	if err != nil {
		return err
	}
	return c.empty(ctx, call{method: http.MethodPost, path: path, body: jsonBody(map[string]string{"user_id": userID})})
}

// Kick removes a user, with a reason when one is given.
func (c *Client) Kick(ctx context.Context, roomID, userID, reason string) error {
	path, err := pathf(clientPrefix+"/rooms/%s/kick", roomID)
	if err != nil {
		return err
	}
	body := map[string]string{"user_id": userID}
	if reason != "" {
		body["reason"] = reason
	}
	return c.empty(ctx, call{method: http.MethodPost, path: path, body: jsonBody(body)})
}

// Membership is a user's membership of the room, or empty when the room has none for them
// (SPEC M33).
func (c *Client) Membership(ctx context.Context, roomID, userID string) (string, error) {
	path, err := pathf(clientPrefix+"/rooms/%s/state/m.room.member/%s", roomID, userID)
	if err != nil {
		return "", err
	}
	cl := call{method: http.MethodGet, path: path}
	var out struct {
		Membership *string `json:"membership"`
	}
	err = c.object(ctx, cl, &out)
	switch {
	case errors.Is(err, ErrNotFound):
		return "", nil
	case err != nil:
		return "", err
	case out.Membership == nil || *out.Membership == "":
		return "", cl.malformed()
	}
	return *out.Membership, nil
}

// JoinedRooms are the rooms the user is joined to (SPEC M35).
func (c *Client) JoinedRooms(ctx context.Context) ([]string, error) {
	cl := call{method: http.MethodGet, path: clientPrefix + "/joined_rooms"}
	var out struct {
		JoinedRooms *[]string `json:"joined_rooms"`
	}
	if err := c.object(ctx, cl, &out); err != nil {
		return nil, err
	}
	if out.JoinedRooms == nil {
		return nil, cl.malformed()
	}
	for _, id := range *out.JoinedRooms {
		if !roomIDRE.MatchString(id) {
			return nil, cl.malformed()
		}
	}
	return *out.JoinedRooms, nil
}

// StateEvent is one state event of a new room's initial state.
type StateEvent struct {
	Type     string          `json:"type"`
	StateKey string          `json:"state_key"`
	Content  json.RawMessage `json:"content"`
}

// CreateRoomRequest is a createRoom body; an empty field is not sent.
type CreateRoomRequest struct {
	Preset             string          `json:"preset,omitempty"`
	RoomVersion        string          `json:"room_version,omitempty"`
	Visibility         string          `json:"visibility,omitempty"`
	Name               string          `json:"name,omitempty"`
	Topic              string          `json:"topic,omitempty"`
	PowerLevelOverride json.RawMessage `json:"power_level_content_override,omitempty"`
	InitialState       []StateEvent    `json:"initial_state,omitempty"`
	Invite             []string        `json:"invite,omitempty"`
}

// CreateRoom makes a room and gives its ID (SPEC M34).
func (c *Client) CreateRoom(ctx context.Context, req CreateRoomRequest) (string, error) {
	if req.PowerLevelOverride != nil {
		if err := rawObject(req.PowerLevelOverride); err != nil {
			return "", err
		}
	}
	for _, s := range req.InitialState {
		if s.Type == "" {
			return "", invalid("an initial state event has no type")
		}
		if err := rawObject(s.Content); err != nil {
			return "", err
		}
	}
	return c.roomID(ctx, call{method: http.MethodPost, path: clientPrefix + "/createRoom", body: jsonBody(req)})
}

// Direction is the order /messages reads in.
type Direction string

// The two directions.
const (
	Forward  Direction = "f"
	Backward Direction = "b"
)

// MessagesRequest is one page of /messages (SPEC M25): From and To are batch tokens, empty for
// none, and Types, when not nil, the event types wanted.
type MessagesRequest struct {
	From  string
	To    string
	Dir   Direction
	Limit int
	Types []string
}

// MessagesPage is one page of a room's events; End is empty when the server sent none (no
// more to read).
type MessagesPage struct {
	Chunk []json.RawMessage
	Start string
	End   string
}

// Messages reads a page of the room's events.
func (c *Client) Messages(ctx context.Context, roomID string, req MessagesRequest) (MessagesPage, error) {
	if req.Dir != Forward && req.Dir != Backward {
		return MessagesPage{}, invalid("dir is f or b")
	}
	if req.Limit < 1 {
		return MessagesPage{}, invalid("the limit is not positive")
	}
	query := url.Values{"dir": {string(req.Dir)}, "limit": {strconv.Itoa(req.Limit)}}
	for key, tok := range map[string]string{"from": req.From, "to": req.To} {
		if tok == "" {
			continue
		}
		if !isToken(tok) {
			return MessagesPage{}, invalid("a batch token is not printable ASCII")
		}
		query.Set(key, tok)
	}
	if req.Types != nil {
		query.Set("filter", string(jsonBody(map[string][]string{"types": req.Types})))
	}
	path, err := pathf(clientPrefix+"/rooms/%s/messages", roomID)
	if err != nil {
		return MessagesPage{}, err
	}
	cl := call{method: http.MethodGet, path: path, query: query}
	var out struct {
		Chunk *[]json.RawMessage `json:"chunk"`
		Start string             `json:"start"`
		End   *string            `json:"end"`
	}
	if err := c.object(ctx, cl, &out); err != nil {
		return MessagesPage{}, err
	}
	if out.Chunk == nil || !allObjects(*out.Chunk) {
		return MessagesPage{}, cl.malformed()
	}
	page := MessagesPage{Chunk: *out.Chunk, Start: out.Start}
	if out.End != nil {
		page.End = *out.End
	}
	return page, nil
}
