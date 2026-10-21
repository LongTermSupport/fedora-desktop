package matrix

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
)

// LoginDevice signs in with a password as the named device (SPEC M45): a pool seat's fixed
// device, so the login replaces that device's token and the previous holder's dies. It sends no
// token, no display name and no refresh token; the answer must name the device asked for.
func (c *Client) LoginDevice(ctx context.Context, userName, password, deviceID string) (Session, error) {
	if deviceID == "" || len(deviceID) > maxSegmentBytes || !printable(deviceID, 0x21) {
		return Session{}, invalid("a device ID is 1 to 255 bytes of printable ASCII without a space")
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

// Logout ends the token and its device (SPEC M46). A token already dead is ErrAuthRefused with
// Errcode M_UNKNOWN_TOKEN; whether that counts as done is the caller's choice.
func (c *Client) Logout(ctx context.Context) error {
	return c.empty(ctx, call{method: http.MethodPost, path: clientPrefix + "/logout", body: []byte(`{}`)})
}

func accountDataPath(userID, roomID, dataType string) (string, error) {
	return pathf(clientPrefix+"/user/%s/rooms/%s/account_data/%s", userID, roomID, dataType)
}

// RoomAccountData is the content of an account's room account data of a type, found=false when
// there is none (SPEC M47). It is handed on as raw JSON, one object, for the caller to decode.
func (c *Client) RoomAccountData(ctx context.Context, userID, roomID, dataType string) (json.RawMessage, bool, error) {
	path, err := accountDataPath(userID, roomID, dataType)
	if err != nil {
		return nil, false, err
	}
	var out json.RawMessage
	err = c.object(ctx, call{method: http.MethodGet, path: path}, &out)
	switch {
	case errors.Is(err, ErrNotFound):
		return nil, false, nil
	case err != nil:
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
