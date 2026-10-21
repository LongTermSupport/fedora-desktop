// Package matrix is the Matrix client-server client both sides of teamchat (the v2 agent bus)
// use, and the Synapse-compatible admin endpoints the host needs (Plan 00209 SPEC-matrix.md
// M1-M36, M45-M47, M49, M50 and M52).
//
// It moves JSON and maps failures. It validates nothing an event carries: events, state content
// and invite state are handed on as raw JSON for the caller to judge. It checks only what the
// transport needs: a path segment is safe to send, and an answer is the shape the endpoint
// promises.
//
// The transport follows no redirect and no proxy, sends the token only in the Authorization
// header of the request it was given to, caps every answer, and never puts the token, a query
// or the server's error text into an error. A 429 is waited out through an injected Clock.
// net/http itself resends a GET, once, when a reused keep-alive connection closes before any
// answer byte; a dropped held sync is then held again rather than reported. That resend is
// safe: a sync with a since token is idempotent, so the resent request is answered from the same
// point in the timeline and nothing is skipped or delivered twice.
package matrix

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

const (
	maxAnswerBytes   = 32 << 20
	maxHeaderBytes   = 64 << 10
	maxTokenBytes    = 4096
	maxSegmentBytes  = 255
	memberLimit      = 15 * time.Second
	adminLimit       = 30 * time.Second
	syncGrace        = 15 * time.Second
	dialLimit        = 10 * time.Second
	maxTries         = 3
	maxRateWait      = 60 * time.Second
	defaultRateWait  = 5 * time.Second
	memberAgent      = "teamchat"
	adminAgent       = "teamchat-admin"
	contentTypeJSON  = "application/json"
	clientPrefix     = "/_matrix/client/v3"
	detailCancelled  = "cancelled"
	detailNoAnswer   = "no answer"
	detailMalformed  = "the answer is not the shape the endpoint gives"
	detailNotObject  = "the answer is not a JSON object"
	detailNoToken    = "no access token"
	detailBadSegment = "a path segment is empty, not printable ASCII or over 255 bytes"
)

var (
	errcodeRE = regexp.MustCompile(`^M_[A-Z0-9_]{1,64}$`)
	agentRE   = regexp.MustCompile(`^[\x21-\x7e][\x20-\x7e]{0,127}$`)
)

// Clock is how the client waits: between 429 retries and between send retries. Sleep returns
// early with the context's error when the context ends.
type Clock interface {
	Sleep(ctx context.Context, d time.Duration) error
}

type systemClock struct{}

// SystemClock is the real clock.
func SystemClock() Clock { return systemClock{} }

func (systemClock) Sleep(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// Config is what a client is built from.
type Config struct {
	// BaseURL is the homeserver: scheme, host and port, nothing after.
	BaseURL string
	// PlainHTTPHosts are the IP literals a member client may reach over plain http; an admin
	// client takes none, as it only reaches 127.0.0.1.
	PlainHTTPHosts []string
	// Token is the access token; an admin client may start without one.
	Token string
	// Clock is how the client waits; nil is SystemClock.
	Clock Clock
	// UserAgent replaces the side's default User-Agent.
	UserAgent string
}

// Client is a Matrix client-server API client. It is safe for concurrent use.
type Client struct {
	base  string
	http  *http.Client
	clock Clock
	agent string
	limit time.Duration
	// authorize sets the Authorization header; nil when the client has no token. A closure,
	// so no formatting of the struct can reach the token.
	authorize func(http.Header)
}

// NewMember builds a member-side client: the bundle's base URL rule (SPEC M14) and a token.
func NewMember(cfg Config) (*Client, error) {
	base, err := memberBase(cfg.BaseURL, cfg.PlainHTTPHosts)
	if err != nil {
		return nil, err
	}
	if cfg.Token == "" {
		return nil, invalid("a member client needs an access token")
	}
	return newClient(cfg, base, memberAgent, memberLimit)
}

func newClient(cfg Config, base, agent string, limit time.Duration) (*Client, error) {
	if cfg.UserAgent != "" {
		if !agentRE.MatchString(cfg.UserAgent) {
			return nil, invalid("the User-Agent is not one line of printable ASCII")
		}
		agent = cfg.UserAgent
	}
	clock := cfg.Clock
	if clock == nil {
		clock = SystemClock()
	}
	c := &Client{base: base, http: newHTTPClient(), clock: clock, agent: agent, limit: limit}
	if cfg.Token == "" {
		return c, nil
	}
	return c.WithToken(cfg.Token)
}

func newHTTPClient() *http.Client {
	return &http.Client{
		Transport: &http.Transport{
			Proxy:                  nil,
			DialContext:            (&net.Dialer{Timeout: dialLimit}).DialContext,
			MaxResponseHeaderBytes: maxHeaderBytes,
			DisableCompression:     true,
			TLSHandshakeTimeout:    dialLimit,
			IdleConnTimeout:        time.Minute,
		},
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
}

// WithToken is a client like c that sends token; the two share their connections.
func (c *Client) WithToken(token string) (*Client, error) {
	if !isToken(token) {
		return nil, invalid("an access token is one line of printable ASCII, at most 4096 bytes")
	}
	bearer := "Bearer " + token
	out := *c
	out.authorize = func(h http.Header) { h.Set("Authorization", bearer) }
	return &out, nil
}

// Close releases the idle connections.
func (c *Client) Close() { c.http.CloseIdleConnections() }

// Format prints the client without its token, whatever the verb.
func (c *Client) Format(f fmt.State, _ rune) {
	_, err := fmt.Fprintf(f, "matrix.Client{%s, token: %v}", c.base, c.authorize != nil)
	if err != nil {
		return
	}
}

// Token is an access token. Formatting it never shows it; Reveal does.
type Token string

// Reveal is the token itself, for the one place that stores or sends it.
func (t Token) Reveal() string { return string(t) }

// Format prints a placeholder, whatever the verb.
func (t Token) Format(f fmt.State, _ rune) {
	if _, err := io.WriteString(f, "[token]"); err != nil {
		return
	}
}

// MarshalText refuses, so a token cannot reach JSON or text by accident.
func (Token) MarshalText() ([]byte, error) {
	return nil, errors.New("matrix: a token is not marshalled; store Reveal() deliberately")
}

// Session is a signed-in device.
type Session struct {
	UserID      string
	AccessToken Token
	DeviceID    string
}

type kindError struct {
	text string
	exit int
}

func (k *kindError) Error() string { return "matrix: " + k.text }

// The failure classes (SPEC M10); errors.Is tests an *Error against them.
var (
	ErrAuthRefused = &kindError{"the access token was refused", 8}
	ErrForbidden   = &kindError{"forbidden", 10}
	ErrNotFound    = &kindError{"not found", 7}
	ErrRateLimited = &kindError{"rate limited", 9}
	ErrUnreachable = &kindError{"the homeserver did not answer usably", 7}
	ErrRefused     = &kindError{"the homeserver refused", 7}
	ErrInvalid     = &kindError{"refused before a request", 7}
)

// Error is a failed call. It carries the method, the path as sent (never the query), the HTTP
// status (0 when there was no answer), the errcode when it is a well-formed one, and a fixed
// detail this package wrote; never the token, the server's error text or a lower-level error's
// text (SPEC M9).
type Error struct {
	Method  string
	Path    string
	Status  int
	Errcode string
	Detail  string

	kind      error
	transient bool
	cause     error
}

func (e *Error) Error() string {
	var b strings.Builder
	b.WriteString("matrix: ")
	if e.Method != "" {
		b.WriteString(e.Method + " " + e.Path + ": ")
	}
	if e.Status != 0 {
		b.WriteString(strconv.Itoa(e.Status))
		if e.Errcode != "" {
			b.WriteString(" " + e.Errcode)
		}
		b.WriteString(": ")
	}
	b.WriteString(e.Detail)
	return b.String()
}

// Is matches the failure class.
func (e *Error) Is(target error) bool { return target == e.kind }

// Unwrap is the caller's context error when the call was cancelled, else nil.
func (e *Error) Unwrap() error { return e.cause }

// Transient says whether the same call may succeed later.
func (e *Error) Transient() bool { return e.transient }

// ExitCode is the exit code a command ends with for this failure.
func (e *Error) ExitCode() term.ExitCode {
	if k, ok := errors.AsType[*kindError](e.kind); ok {
		return term.ExitCode(k.exit)
	}
	return term.ExitCode(ErrRefused.exit)
}

func invalid(detail string) *Error {
	return &Error{Detail: detail, kind: ErrInvalid}
}

// call is one endpoint request.
type call struct {
	method string
	// path is percent-encoded already.
	path  string
	query url.Values
	body  []byte
	// noAuth sends no token.
	noAuth bool
	// limit replaces the client's per-try time limit.
	limit time.Duration
}

func (cl call) fail(kind error, status int, errcode, detail string, transient bool) *Error {
	return &Error{
		Method: cl.method, Path: cl.path, Status: status, Errcode: errcode, Detail: detail,
		kind: kind, transient: transient,
	}
}

func (cl call) cancelled(cause error) *Error {
	e := cl.fail(ErrUnreachable, 0, "", detailCancelled, false)
	e.cause = cause
	return e
}

func (cl call) malformed() *Error {
	return cl.fail(ErrUnreachable, http.StatusOK, "", detailMalformed, false)
}

// isToken is the grammar of a token, a batch token and a nonce: one line of printable ASCII,
// at most 4096 bytes (SPEC M4).
func isToken(s string) bool {
	return s != "" && len(s) <= maxTokenBytes && printable(s, 0x21)
}

// printable says every byte of s is from low to '~'.
func printable(s string, low byte) bool {
	for i := range len(s) {
		if s[i] < low || s[i] > 0x7e {
			return false
		}
	}
	return true
}

type answer struct {
	status int
	header http.Header
	body   []byte
}

// do sends the call, waiting out 429s (SPEC M16), and gives the body of its 200 answer.
func (c *Client) do(ctx context.Context, cl call) ([]byte, error) {
	ans, err := c.answered(ctx, cl)
	if err != nil {
		return nil, err
	}
	return cl.judge(ans)
}

// answered sends the call, waiting out 429s, and gives the first answer that is not a 429.
func (c *Client) answered(ctx context.Context, cl call) (*answer, error) {
	if !cl.noAuth && c.authorize == nil {
		return nil, cl.fail(ErrInvalid, 0, "", detailNoToken, false)
	}
	for try := 1; ; try++ {
		ans, err := c.once(ctx, cl)
		if err != nil {
			return nil, err
		}
		if ans.status != http.StatusTooManyRequests {
			return ans, nil
		}
		wait := rateWait(ans)
		if try == maxTries || wait > maxRateWait {
			return nil, cl.fail(ErrRateLimited, ans.status, errcodeOf(ans.body), "rate limited", true)
		}
		if err := c.clock.Sleep(ctx, wait); err != nil {
			return nil, cl.cancelled(ctx.Err())
		}
	}
}

func (c *Client) once(ctx context.Context, cl call) (*answer, error) {
	if err := ctx.Err(); err != nil {
		return nil, cl.cancelled(err)
	}
	limit := c.limit
	if cl.limit != 0 {
		limit = cl.limit
	}
	tryCtx, cancel := context.WithTimeout(ctx, limit)
	defer cancel()
	target := c.base + cl.path
	if len(cl.query) > 0 {
		target += "?" + cl.query.Encode()
	}
	var body io.Reader
	if cl.body != nil {
		body = bytes.NewReader(cl.body)
	}
	req, err := http.NewRequestWithContext(tryCtx, cl.method, target, body)
	if err != nil {
		return nil, cl.fail(ErrInvalid, 0, "", "the request could not be built", false)
	}
	req.Header.Set("Accept", contentTypeJSON)
	req.Header.Set("User-Agent", c.agent)
	if cl.body != nil {
		req.Header.Set("Content-Type", contentTypeJSON)
	}
	if !cl.noAuth {
		c.authorize(req.Header)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, cl.lost(ctx, tryCtx, detailNoAnswer)
	}
	data, readErr := io.ReadAll(io.LimitReader(resp.Body, maxAnswerBytes+1))
	closeErr := resp.Body.Close()
	switch {
	case readErr != nil:
		return nil, cl.lost(ctx, tryCtx, "answer cut short")
	case len(data) > maxAnswerBytes:
		return nil, cl.fail(ErrUnreachable, resp.StatusCode, "", "answer over 33554432 bytes", false)
	case closeErr != nil:
		return nil, cl.lost(ctx, tryCtx, "answer cut short")
	}
	return &answer{status: resp.StatusCode, header: resp.Header, body: data}, nil
}

// lost is a request that got no whole answer: the caller's cancellation, this client's time
// limit, or the connection.
func (cl call) lost(ctx, tryCtx context.Context, detail string) *Error {
	if err := ctx.Err(); err != nil {
		return cl.cancelled(err)
	}
	if tryCtx.Err() != nil {
		return cl.fail(ErrUnreachable, 0, "", "timed out", true)
	}
	return cl.fail(ErrUnreachable, 0, "", detail, true)
}

func (cl call) judge(ans *answer) ([]byte, error) {
	code := errcodeOf(ans.body)
	switch s := ans.status; {
	case s == http.StatusOK:
		return ans.body, nil
	case s >= 200 && s < 300:
		return nil, cl.fail(ErrRefused, s, code, "an unexpected success status", false)
	case s >= 300 && s < 400:
		return nil, cl.fail(ErrRefused, s, code, "redirect refused", false)
	case s == http.StatusUnauthorized:
		return nil, cl.fail(ErrAuthRefused, s, code, "the access token was refused", false)
	case s == http.StatusForbidden:
		return nil, cl.fail(ErrForbidden, s, code, "forbidden", false)
	case s == http.StatusNotFound:
		return nil, cl.fail(ErrNotFound, s, code, "not found", false)
	case s >= 500:
		return nil, cl.fail(ErrRefused, s, code, "server error", true)
	default:
		return nil, cl.fail(ErrRefused, s, code, "refused", false)
	}
}

func errcodeOf(body []byte) string {
	var v struct {
		Errcode string `json:"errcode"`
	}
	if json.Unmarshal(body, &v) != nil || !errcodeRE.MatchString(v.Errcode) {
		return ""
	}
	return v.Errcode
}

// rateWait is the wait a 429 asks for: Retry-After in whole seconds, else the body's
// retry_after_ms, else 5 s. A wait too long to represent is over the cap.
func rateWait(ans *answer) time.Duration {
	over := maxRateWait + time.Second
	if v := strings.TrimSpace(ans.header.Get("Retry-After")); v != "" && digits(v) {
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil || n > int64(maxRateWait/time.Second) {
			return over
		}
		return time.Duration(n) * time.Second
	}
	var body struct {
		RetryAfterMS json.RawMessage `json:"retry_after_ms"`
	}
	if json.Unmarshal(ans.body, &body) == nil && digits(string(body.RetryAfterMS)) {
		ms, err := strconv.ParseInt(string(body.RetryAfterMS), 10, 64)
		if err != nil || ms > maxRateWait.Milliseconds() {
			return over
		}
		return time.Duration(ms) * time.Millisecond
	}
	return defaultRateWait
}

func digits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// object sends the call and decodes its answer, which must be one JSON object, into out.
func (c *Client) object(ctx context.Context, cl call, out any) error {
	body, err := c.do(ctx, cl)
	if err != nil {
		return err
	}
	return objectBody(cl, body, out)
}

// objectBody decodes an answer's body, which must be one JSON object, into out.
func objectBody(cl call, body []byte, out any) error {
	if first(body) != '{' {
		return cl.fail(ErrUnreachable, http.StatusOK, "", detailNotObject, false)
	}
	return decode(cl, body, out)
}

// array is object for an answer that is one JSON array.
func (c *Client) array(ctx context.Context, cl call, out any) error {
	body, err := c.do(ctx, cl)
	if err != nil {
		return err
	}
	if first(body) != '[' {
		return cl.fail(ErrUnreachable, http.StatusOK, "", "the answer is not a JSON array", false)
	}
	return decode(cl, body, out)
}

func decode(cl call, body []byte, out any) error {
	dec := json.NewDecoder(bytes.NewReader(body))
	if err := dec.Decode(out); err != nil {
		return cl.malformed()
	}
	if dec.More() {
		return cl.malformed()
	}
	return nil
}

func first(body []byte) byte {
	trimmed := bytes.TrimLeft(body, " \t\r\n")
	if len(trimmed) == 0 {
		return 0
	}
	return trimmed[0]
}

// jsonBody marshals a request body the package built itself.
func jsonBody(v any) []byte {
	out, err := json.Marshal(v)
	if err != nil {
		panic(fmt.Sprintf("matrix: a request body did not marshal: %v", err))
	}
	return out
}

// rawObject checks that raw is one JSON object, for content the caller passes (SPEC M26).
func rawObject(raw json.RawMessage) error {
	if first(raw) != '{' || !json.Valid(raw) {
		return invalid("the content is not one JSON object")
	}
	return nil
}

// escape percent-encodes every byte but [A-Za-z0-9_.~-] (SPEC M12).
func escape(s string) string {
	var b strings.Builder
	for i := range len(s) {
		ch := s[i]
		if ch >= 'A' && ch <= 'Z' || ch >= 'a' && ch <= 'z' || ch >= '0' && ch <= '9' || strings.IndexByte("_.~-", ch) >= 0 {
			b.WriteByte(ch)
			continue
		}
		fmt.Fprintf(&b, "%%%02X", ch)
	}
	return b.String()
}

func segmentOK(s string, emptyOK bool) bool {
	if len(s) > maxSegmentBytes || s == "" && !emptyOK {
		return false
	}
	for i := range len(s) {
		if s[i] < 0x20 || s[i] > 0x7e {
			return false
		}
	}
	return true
}

var (
	adminBaseRE = regexp.MustCompile(`^http://127\.0\.0\.1:([1-9][0-9]{0,4})$`)
	dnsLabelRE  = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$`)
)

// adminBase takes only http://127.0.0.1:<port>, the port canonical and 1 to 65535 (SPEC M15).
func adminBase(raw string) (string, error) {
	m := adminBaseRE.FindStringSubmatch(raw)
	if m == nil {
		return "", invalid("the admin base URL is not http://127.0.0.1:<port>")
	}
	if port, err := strconv.Atoi(m[1]); err != nil || port > 65535 {
		return "", invalid("the admin base URL's port is not 1 to 65535")
	}
	return raw, nil
}

// memberBase applies the bundle's base URL rule (SPEC M14) and gives the URL with its host
// lower-cased.
func memberBase(raw string, plainHosts []string) (string, error) {
	u, err := parseBase(raw)
	if err != nil {
		return "", err
	}
	host := strings.ToLower(u.Hostname())
	ip := net.ParseIP(host)
	switch {
	case ip != nil && (strings.Contains(host, "%") || canonicalIP(ip) != host):
		return "", invalid("the base URL's IP literal is not canonical")
	case u.Scheme == "http" && (ip == nil || !listed(host, plainHosts)):
		return "", invalid("plain http is only for an IP literal in plain_http_hosts")
	case ip == nil && !dnsName(host):
		return "", invalid("the base URL's host is not a DNS name or an IP literal")
	}
	out := url.URL{Scheme: u.Scheme, Host: hostPort(host, ip != nil, u.Port())}
	return out.String(), nil
}

// parseBase is the base URL's shape: printable ASCII, scheme http or https, a host and a valid
// port, and nothing else.
func parseBase(raw string) (*url.URL, error) {
	if !printable(raw, 0x21) {
		return nil, invalid("the base URL is not printable ASCII")
	}
	u, err := url.Parse(raw)
	if err != nil || strings.ContainsAny(raw, "?#") || u.Opaque != "" || u.User != nil || u.Path != "" || u.RawPath != "" {
		return nil, invalid("the base URL is not scheme://host[:port] alone")
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return nil, invalid("the base URL's scheme is not http or https")
	}
	if u.Hostname() == "" {
		return nil, invalid("the base URL has no host")
	}
	return u, checkPort(u)
}

func checkPort(u *url.URL) error {
	if !strings.HasSuffix(u.Host, ":") && u.Port() == "" {
		return nil
	}
	p := u.Port()
	port, err := strconv.Atoi(p)
	if err != nil || p[0] == '0' || port < 1 || port > 65535 {
		return invalid("the base URL's port is not 1 to 65535")
	}
	return nil
}

func canonicalIP(ip net.IP) string {
	if v4 := ip.To4(); v4 != nil && !strings.Contains(ip.String(), ":") {
		return v4.String()
	}
	return ip.String()
}

func listed(host string, hosts []string) bool {
	for _, h := range hosts {
		if strings.EqualFold(h, host) {
			return true
		}
	}
	return false
}

func dnsName(host string) bool {
	if len(host) > 253 {
		return false
	}
	for label := range strings.SplitSeq(host, ".") {
		if !dnsLabelRE.MatchString(label) {
			return false
		}
	}
	return true
}

func hostPort(host string, isIP bool, port string) string {
	if isIP && strings.Contains(host, ":") {
		host = "[" + host + "]"
	}
	if port == "" {
		return host
	}
	return host + ":" + port
}

// pathf fills each %s of format with a segment, refused unless printable ASCII of 1 to 255
// bytes, and percent-encoded.
func pathf(format string, segs ...string) (string, error) {
	return pathKey(format, false, segs...)
}

// pathKey is pathf whose last segment, a state key, may be empty.
func pathKey(format string, lastEmptyOK bool, segs ...string) (string, error) {
	args := make([]any, len(segs))
	for i, s := range segs {
		if !segmentOK(s, lastEmptyOK && i == len(segs)-1) {
			return "", invalid(detailBadSegment)
		}
		args[i] = escape(s)
	}
	return fmt.Sprintf(format, args...), nil
}
