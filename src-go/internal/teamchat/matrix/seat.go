package matrix

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
)

const (
	detailNoPasswordFlow  = "the server offers no password-only auth"
	detailPasswordRefused = "the password was refused"
	errcodeForbidden      = "M_FORBIDDEN"
)

// checkDeviceID refuses a device ID that is not 1 to 255 bytes of printable ASCII without a
// space.
func checkDeviceID(deviceID string) error {
	if deviceID == "" || len(deviceID) > maxSegmentBytes || !printable(deviceID, 0x21) {
		return invalid("a device ID is 1 to 255 bytes of printable ASCII without a space")
	}
	return nil
}

// LoginDevice signs in with a password as the named device (SPEC M45): a pool seat's fixed
// device. On Tuwunel the device's earlier tokens stay valid (host check H-C13), so a take ends
// them by Logout or DeleteDevice. It sends no token, no display name and no refresh token; the
// answer must name the device asked for.
func (c *Client) LoginDevice(ctx context.Context, userName, password, deviceID string) (Session, error) {
	if err := checkDeviceID(deviceID); err != nil {
		return Session{}, err
	}
	body := passwordLogin(userName, password)
	body["device_id"] = deviceID
	cl := loginCall(body)
	sess, err := c.session(ctx, cl)
	if err != nil {
		return Session{}, err
	}
	if sess.DeviceID != deviceID {
		return Session{}, cl.malformed()
	}
	return sess, nil
}

// Logout ends every token of the client's device, and the device (SPEC M46). A token already
// dead is ErrAuthRefused with Errcode M_UNKNOWN_TOKEN; whether that counts as done is the
// caller's choice.
func (c *Client) Logout(ctx context.Context) error {
	return c.empty(ctx, call{method: http.MethodPost, path: clientPrefix + "/logout", body: []byte(`{}`)})
}

func accountDataPath(userID, roomID, dataType string) (string, error) {
	return pathf(clientPrefix+"/user/%s/rooms/%s/account_data/%s", userID, roomID, dataType)
}

// RoomAccountData is the content of an account's room account data of a type, found=false when
// there is none: a 404 M_NOT_FOUND (SPEC M47). Any other 404 is the error, as it may be a server
// that does not serve the endpoint. The content is handed on as raw JSON, one object, for the
// caller to decode.
func (c *Client) RoomAccountData(ctx context.Context, userID, roomID, dataType string) (json.RawMessage, bool, error) {
	path, err := accountDataPath(userID, roomID, dataType)
	if err != nil {
		return nil, false, err
	}
	var out json.RawMessage
	err = c.object(ctx, call{method: http.MethodGet, path: path}, &out)
	if e, failed := errors.AsType[*Error](err); failed && errors.Is(err, ErrNotFound) && e.Errcode == "M_NOT_FOUND" {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	return out, true, nil
}

// PutRoomAccountData sets an account's room account data of a type; the content must be one JSON
// object (SPEC M47).
func (c *Client) PutRoomAccountData(ctx context.Context, userID, roomID, dataType string, content json.RawMessage) error {
	if err := rawObject(content); err != nil {
		return err
	}
	path, err := accountDataPath(userID, roomID, dataType)
	if err != nil {
		return err
	}
	return c.empty(ctx, call{method: http.MethodPut, path: path, body: content})
}

// DeleteDevice deletes one of the account's devices, which ends every token of it (SPEC M52):
// how a take ends a previous holder's token, which a second login as the device does not (host
// check H-C13). The first request is answered with a user-interactive auth challenge; the
// second answers it with userName's password in the one-stage m.login.password flow. A wrong
// password is ErrForbidden; a server that asks no auth is answered at once.
func (c *Client) DeleteDevice(ctx context.Context, userName, password, deviceID string) error {
	if err := checkDeviceID(deviceID); err != nil {
		return err
	}
	cl := call{method: http.MethodDelete, path: clientPrefix + "/devices/" + escape(deviceID), body: []byte(`{}`)}
	ans, err := c.answered(ctx, cl)
	if err != nil {
		return err
	}
	if ans.status != http.StatusUnauthorized || errcodeOf(ans.body) != "" {
		return emptyAnswer(cl, ans)
	}
	session, err := passwordSession(cl, ans.body)
	if err != nil {
		return err
	}
	auth := passwordLogin(userName, password)
	auth["session"] = session
	cl.body = jsonBody(map[string]any{"auth": auth})
	if ans, err = c.answered(ctx, cl); err != nil {
		return err
	}
	if ans.status == http.StatusUnauthorized && errcodeOf(ans.body) == errcodeForbidden {
		return cl.fail(ErrForbidden, ans.status, errcodeForbidden, detailPasswordRefused, false)
	}
	return emptyAnswer(cl, ans)
}

// emptyAnswer judges an answer whose success is one JSON object the caller does not read.
func emptyAnswer(cl call, ans *answer) error {
	body, err := cl.judge(ans)
	if err != nil {
		return err
	}
	var out map[string]json.RawMessage
	return objectBody(cl, body, &out)
}

// passwordSession is the session of a user-interactive auth challenge that offers the
// one-stage m.login.password flow.
func passwordSession(cl call, body []byte) (string, error) {
	var challenge struct {
		Session string `json:"session"`
		Flows   []struct {
			Stages []string `json:"stages"`
		} `json:"flows"`
	}
	if err := objectBody(cl, body, &challenge); err != nil {
		return "", cl.malformed()
	}
	if !isToken(challenge.Session) {
		return "", cl.malformed()
	}
	for _, flow := range challenge.Flows {
		if len(flow.Stages) == 1 && flow.Stages[0] == "m.login.password" {
			return challenge.Session, nil
		}
	}
	return "", cl.fail(ErrRefused, http.StatusUnauthorized, "", detailNoPasswordFlow, false)
}
