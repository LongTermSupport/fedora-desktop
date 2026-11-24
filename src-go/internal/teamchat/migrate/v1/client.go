package v1

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strconv"
)

// Doer sends one request: an *http.Client.
type Doer interface {
	Do(req *http.Request) (*http.Response, error)
}

const (
	clientPrefix = "/_matrix/client/v3"
	pageLimit    = 100
	// maxResponse caps the body of one answer.
	maxResponse = 64 << 20
)

// errcodeGrammar is a Matrix error code safe to repeat in an error.
var errcodeGrammar = regexp.MustCompile(`^M_[A-Z_]{1,64}$`)

// client is the GET-only client of a v1 homeserver on loopback (E7): the token goes only into the
// Authorization header, and no error repeats it or a body the server sent.
type client struct {
	http  Doer
	port  int
	token string
	// limit caps an answer's body; 0 is maxResponse.
	limit int
}

// noAnswerError is a request the homeserver did not answer (E8).
type noAnswerError struct{ err error }

func (e *noAnswerError) Error() string { return "no answer: " + e.err.Error() }

func (e *noAnswerError) Unwrap() error { return e.err }

func (c client) base() string { return "http://127.0.0.1:" + strconv.Itoa(c.port) }

// get sends GET path?query and returns the status and the body.
func (c client) get(ctx context.Context, path string, query url.Values) (int, []byte, error) {
	u := url.URL{Scheme: "http", Host: "127.0.0.1:" + strconv.Itoa(c.port), Path: path, RawQuery: query.Encode()}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), http.NoBody)
	if err != nil {
		return 0, nil, fmt.Errorf("GET %s: %w", path, err)
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	resp, err := c.http.Do(req)
	if err != nil {
		return 0, nil, &noAnswerError{err}
	}
	limit := c.limit
	if limit == 0 {
		limit = maxResponse
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, int64(limit)+1))
	err = errors.Join(err, resp.Body.Close())
	if err != nil {
		return 0, nil, fmt.Errorf("GET %s: reading the answer: %w", path, err)
	}
	if len(body) > limit {
		return 0, nil, fmt.Errorf("GET %s: the answer is more than %d bytes", path, limit)
	}
	return resp.StatusCode, body, nil
}

// getOK is get for an answer that must be 200 and decode into out.
func (c client) getOK(ctx context.Context, path string, query url.Values, out any) ([]byte, error) {
	status, body, err := c.get(ctx, path, query)
	if err != nil {
		return nil, err
	}
	if status != http.StatusOK {
		return nil, statusError(path, status, body)
	}
	if err := json.Unmarshal(body, out); err != nil {
		return nil, fmt.Errorf("GET %s: a malformed answer", path)
	}
	return body, nil
}

// statusError names a status that is not 200, with the Matrix error code when it is one.
func statusError(path string, status int, body []byte) error {
	var e struct {
		Errcode string `json:"errcode"`
	}
	if json.Unmarshal(body, &e) == nil && errcodeGrammar.MatchString(e.Errcode) {
		return fmt.Errorf("GET %s answered HTTP %d (%s)", path, status, e.Errcode)
	}
	return fmt.Errorf("GET %s answered HTTP %d", path, status)
}

// whoami is E9: the token must be the team admin's.
func (c client) whoami(ctx context.Context, serverName string) error {
	path := clientPrefix + "/account/whoami"
	status, body, err := c.get(ctx, path, nil)
	if err != nil {
		return err
	}
	if status == http.StatusUnauthorized || status == http.StatusForbidden {
		return fmt.Errorf("the admin token was rejected (HTTP %d)", status)
	}
	if status != http.StatusOK {
		return statusError(path, status, body)
	}
	var who struct {
		UserID *string `json:"user_id"`
	}
	if json.Unmarshal(body, &who) != nil || who.UserID == nil {
		return fmt.Errorf("GET %s: a malformed answer", path)
	}
	if want := "@admin:" + serverName; *who.UserID != want {
		return fmt.Errorf("the admin token is %q's, not %q's", *who.UserID, want)
	}
	return nil
}

// startToken is E10's starting point: the next_batch of one sync whose filter gives no timeline.
func (c client) startToken(ctx context.Context, roomID string) (string, error) {
	filter, err := json.Marshal(map[string]any{"room": map[string]any{"rooms": []string{roomID}, "timeline": map[string]int{"limit": 0}}})
	if err != nil {
		return "", err
	}
	var out struct {
		NextBatch string `json:"next_batch"`
	}
	path := clientPrefix + "/sync"
	if _, err := c.getOK(ctx, path, url.Values{"filter": {string(filter)}, "timeout": {"0"}}, &out); err != nil {
		return "", err
	}
	if !printable([]byte(out.NextBatch)) {
		return "", fmt.Errorf("GET %s: no usable next_batch", path)
	}
	return out.NextBatch, nil
}

// history is E10: every event of the room, paged backwards from from with no type filter and no
// page cap, in chronological order. A failure on any page fails the whole.
func (c client) history(ctx context.Context, roomID, from string) ([]json.RawMessage, error) {
	path := clientPrefix + "/rooms/" + roomID + "/messages"
	var all []json.RawMessage
	for {
		var page struct {
			Chunk *[]json.RawMessage `json:"chunk"`
			End   *string            `json:"end"`
		}
		query := url.Values{"dir": {"b"}, "limit": {strconv.Itoa(pageLimit)}, "from": {from}}
		if _, err := c.getOK(ctx, path, query, &page); err != nil {
			return nil, err
		}
		if page.Chunk == nil || !allObjects(*page.Chunk) {
			return nil, fmt.Errorf("GET %s: a page that is not a chunk of events", path)
		}
		all = append(all, *page.Chunk...)
		if len(*page.Chunk) == 0 || page.End == nil || *page.End == from {
			break
		}
		if !printable([]byte(*page.End)) {
			return nil, fmt.Errorf("GET %s: an end token that is not printable ASCII", path)
		}
		from = *page.End
	}
	slices.Reverse(all)
	return all, nil
}

// state is E11: the room's current state as the server sent it, which must be an array of
// objects.
func (c client) state(ctx context.Context, roomID string) ([]byte, error) {
	var events []json.RawMessage
	path := clientPrefix + "/rooms/" + roomID + "/state"
	body, err := c.getOK(ctx, path, nil, &events)
	if err != nil {
		return nil, err
	}
	if events == nil || !allObjects(events) {
		return nil, fmt.Errorf("GET %s: not an array of state events", path)
	}
	return body, nil
}

func isObject(m json.RawMessage) bool {
	return bytes.HasPrefix(bytes.TrimLeft(m, " \t\r\n"), []byte("{"))
}

func allObjects(ms []json.RawMessage) bool {
	for _, m := range ms {
		if !isObject(m) {
			return false
		}
	}
	return true
}
