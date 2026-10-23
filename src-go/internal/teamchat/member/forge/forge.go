// Package forge is the member's forge check (Plan 00209 SPEC-n3 SD5; A 9.2-9.3, B 2.6 step
// 11): whether a ping's reference is proven on GitHub, by its provenance and not merely by
// the API returning it, since a fork's commits are served through its parent's API.
//
// One Checker serves every team of a link; its caches and the forge's health are shared and
// safe for concurrent use, and it holds no lock across a request, so one slow check holds
// only its own team (n3d-D20). The credential is gh's, read through execx only when a request
// is about to go out (Login), and sent only as the Authorization header of requests to the
// origin proto fixes for the forge kind; a redirect off that origin is refused before the
// other origin is asked anything.
//
// Results: proven, provenance (found, not proven), unresolved (not found, wrong kind, the
// credential refused) and forge-rate. A forge with no answer (no connection, or 15 s with no
// answer or a 5xx on the repository's own request) is unresolved with ErrDown, and every check
// in the 60 s after it is ErrDown at once, with no request (n3d-D29); the same on a request the
// reference shapes (a compare, a file) is that check's unresolved alone (n3d-D31). Only facts
// that can lead to a proof are cached: one hour for repositories, branches, reachability, pull
// requests and issues; for the link's life for a file at a SHA (n3d-D22); 1024 entries per
// cache, oldest evicted.
package forge

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptrace"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/clock"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/team"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/proto"
)

// Proven is the outcome of a reference the forge proves; the others are proto's reasons
// (ReasonProvenance, ReasonUnresolved, ReasonForgeRate).
const Proven = team.ForgeProven

const (
	cacheEntries   = 1024
	maxBody        = 8 << 20
	maxHeaderBytes = 64 << 10
	maxRedirects   = 5
	requestLimit   = 15 * time.Second
	downFor        = 60 * time.Second
	factLife       = time.Hour
	rateTries      = 3
	maxRateWait    = 60 * time.Second
	// maxRateHold bounds how long one rate answer stops every check: the primary window.
	maxRateHold     = time.Hour
	defaultRateWait = 5 * time.Second
	// maxHeaderSeconds bounds a rate header's number before it becomes a duration.
	maxHeaderSeconds = 1 << 32
)

// Result is one check's outcome: Proven or one of proto's forge reasons. Its trust, when
// proven, is the class's (proto.ForgeCheck.Trust).
type Result = team.ForgeResult

// A Checker is the engine's and teamsync's forge.
var _ team.Forge = (*Checker)(nil)

var (
	// ErrLogin is a missing or logged-out gh: a configuration failure of the forge only.
	ErrLogin = team.ErrForgeLogin
	// ErrDown is a forge that did not answer.
	ErrDown = team.ErrForgeDown

	errOffOrigin = errors.New("forge: a redirect off the forge's origin")
	errRedirects = errors.New("forge: too many redirects")

	shaRe = regexp.MustCompile(`^[0-9a-f]{40}$`)

	trustedAssociations = []string{"OWNER", "MEMBER", "COLLABORATOR"}
)

// outcome ends a check early with a result and no error for the caller.
type outcome string

func (o outcome) Error() string { return "forge: " + string(o) }

// Config is what New needs: the login (required) and the clock (nil is the machine's).
type Config struct {
	Login *Login
	Clock clock.Clock
}

// Checker runs forge checks. Safe for concurrent use by every team of a link.
type Checker struct {
	login  *Login
	clock  clock.Clock
	origin func(kind string) string
	http   *http.Client

	mu         sync.Mutex
	downUntil  time.Duration
	ratedUntil time.Duration
	window     rateWindow
	repos      *cache[repoFacts]
	branches   *cache[struct{}]
	reach      *cache[struct{}]
	pulls      *cache[pullFacts]
	issues     *cache[struct{}]
	files      *cache[struct{}]
}

// New is a checker over cfg.
func New(cfg Config) *Checker {
	c := cfg.Clock
	if c == nil {
		c = clock.System()
	}
	return &Checker{
		login:    cfg.Login,
		clock:    c,
		origin:   proto.ForgeOrigin,
		http:     newHTTPClient(),
		repos:    newCache[repoFacts](factLife),
		branches: newCache[struct{}](factLife),
		reach:    newCache[struct{}](factLife),
		pulls:    newCache[pullFacts](factLife),
		issues:   newCache[struct{}](factLife),
		files:    newCache[struct{}](0),
	}
}

func newHTTPClient() *http.Client {
	return &http.Client{
		Transport: &http.Transport{
			Proxy:                  nil,
			DialContext:            (&net.Dialer{Timeout: requestLimit}).DialContext,
			TLSHandshakeTimeout:    requestLimit,
			MaxResponseHeaderBytes: maxHeaderBytes,
			IdleConnTimeout:        time.Minute,
			ForceAttemptHTTP2:      true,
		},
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			switch {
			case req.URL.Scheme != via[0].URL.Scheme || req.URL.Host != via[0].URL.Host:
				return errOffOrigin
			case len(via) >= maxRedirects:
				return errRedirects
			}
			return nil
		},
	}
}

// Check proves need's reference under the roster's forge section f. The error is ErrDown
// (the forge did not answer, now or within the last 60 s), ErrLogin, the context's error, or
// an unknown forge kind; each comes with outcome unresolved.
func (c *Checker) Check(ctx context.Context, need proto.ForgeCheck, f proto.Forge) (Result, error) {
	unresolved := Result{Outcome: proto.ReasonUnresolved}
	if c.isDown() {
		return unresolved, fmt.Errorf("%w: it did not answer within the last %s", ErrDown, downFor)
	}
	if c.isRated() {
		return Result{Outcome: proto.ReasonForgeRate}, nil
	}
	origin := c.origin(f.Kind)
	if origin == "" {
		return unresolved, fmt.Errorf("forge: no origin for forge kind %q", f.Kind)
	}
	r := &run{c: c, origin: origin, need: need, f: f, base: "/repos/" + need.Ref.OwnerRepo()}
	r.spend, r.receive = team.ReceiveSpend(ctx)
	got, err := r.prove(ctx)
	var o outcome
	switch {
	case err == nil:
		return Result{Outcome: got}, nil
	case errors.As(err, &o):
		return Result{Outcome: string(o)}, nil
	case errors.Is(err, ErrDown):
		c.markDown()
	}
	return unresolved, err
}

func (c *Checker) isDown() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.clock.Boot() < c.downUntil
}

func (c *Checker) markDown() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.downUntil = c.clock.Boot() + downFor
}

// isRated is the rate health beside the down health (n3d-D31): within a rate answer's wait,
// every check is forge-rate at once, with no request.
func (c *Checker) isRated() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.clock.Boot() < c.ratedUntil
}

func (c *Checker) markRated(wait time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.ratedUntil = max(c.ratedUntil, c.clock.Boot()+min(wait, maxRateHold))
}

// passesBound is a wait that would not end before the caller's bound (clock.Bound).
func (c *Checker) passesBound(ctx context.Context, wait time.Duration) bool {
	at, ok := clock.Bound(ctx)
	return ok && c.clock.Boot()+wait >= at
}

// rateWindow is the forge's rate window as its last answer gave it (X-RateLimit-*): the
// requests remaining of limit until the reset, on the wall clock.
type rateWindow struct {
	known            bool
	remaining, limit int64
	resetMS          int64
}

// run is one check: the reference, its forge section and its repository's API path. A receive
// check carries its sender's spend (team.WithReceive), asked before its first request.
type run struct {
	c       *Checker
	origin  string
	need    proto.ForgeCheck
	f       proto.Forge
	base    string
	spend   func() bool
	receive bool
	spent   bool
}

type repoFacts struct {
	FullName string `json:"full_name"`
	Owner    struct {
		Login string `json:"login"`
	} `json:"owner"`
	Fork          bool   `json:"fork"`
	Private       bool   `json:"private"`
	DefaultBranch string `json:"default_branch"`
}

type pullFacts struct {
	Head struct {
		SHA  string `json:"sha"`
		Repo *struct {
			FullName string `json:"full_name"`
		} `json:"repo"`
	} `json:"head"`
	Association string `json:"author_association"`
}

// prove is A 9.3's table: the repository's facts for the class, then the form's proof.
func (r *run) prove(ctx context.Context) (string, error) {
	switch r.need.Class {
	case proto.RefListed, proto.RefOrg, proto.RefExternal:
	case proto.RefExcluded:
		return proto.ReasonUnresolved, nil
	}
	repo, err := r.repo(ctx)
	if err != nil {
		return "", err
	}
	if !r.repoProves(repo) {
		return proto.ReasonProvenance, nil
	}
	switch r.need.Ref.Form {
	case proto.RefCommit:
		return r.commit(ctx, repo)
	case proto.RefPath:
		got, err := r.commit(ctx, repo)
		if err != nil || got != Proven {
			return got, err
		}
		return r.file(ctx)
	case proto.RefPR:
		return r.pull(ctx)
	case proto.RefIssue:
		return r.issue(ctx)
	}
	return proto.ReasonUnresolved, nil
}

func (r *run) repo(ctx context.Context) (repoFacts, error) {
	if v, ok := lookup(r.c, r.c.repos, r.base); ok {
		return v, nil
	}
	var v repoFacts
	found, err := r.fetch(ctx, r.base, &v)
	switch {
	case err != nil:
		return v, err
	case !found:
		return v, outcome(proto.ReasonUnresolved)
	}
	if r.repoProves(v) {
		keep(r.c, r.c.repos, r.base, v)
	}
	return v, nil
}

// repoProves is the repository column: listed by its owner and name, org by its owner's
// login, external by being public; never a fork.
func (r *run) repoProves(v repoFacts) bool {
	switch r.need.Class {
	case proto.RefListed:
		return strings.EqualFold(v.FullName, r.need.Ref.OwnerRepo()) && !v.Fork
	case proto.RefOrg:
		return strings.EqualFold(v.Owner.Login, r.f.Org) && !v.Fork
	case proto.RefExternal:
		return !v.Private && !v.Fork
	case proto.RefExcluded:
	}
	return false
}

// commit is the commit proof: the SHA reached from a proving branch. A compare the forge
// cannot answer for any branch is a SHA it does not know (unresolved); one it answers
// without reaching is provenance.
func (r *run) commit(ctx context.Context, repo repoFacts) (string, error) {
	bases, early, err := r.provingBranches(ctx, repo)
	if err != nil || early != "" {
		return early, err
	}
	seen := false
	for _, b := range bases {
		reached, found, err := r.reaches(ctx, b, r.need.Ref.SHA, "identical", "behind")
		if err != nil {
			return "", err
		}
		if reached {
			return Proven, nil
		}
		seen = seen || found
	}
	if !seen {
		return proto.ReasonUnresolved, nil
	}
	return proto.ReasonProvenance, nil
}

// provingBranches is the branch column, or the outcome that ends the check before any compare.
func (r *run) provingBranches(ctx context.Context, repo repoFacts) ([]string, string, error) {
	b := r.need.Branch
	switch {
	case r.need.Branches != nil && b == "":
		return slices.DeleteFunc(slices.Clone(r.need.Branches), func(s string) bool { return !branchSafe(s) }), "", nil
	case r.need.Branches != nil && !slices.Contains(r.need.Branches, b):
		return nil, proto.ReasonProvenance, nil
	case b == "" && repo.DefaultBranch == "":
		return nil, proto.ReasonUnresolved, nil
	case b == "":
		return []string{repo.DefaultBranch}, "", nil
	case !branchSafe(b):
		return nil, proto.ReasonProvenance, nil
	}
	path := r.base + "/branches/" + b
	if _, ok := lookup(r.c, r.c.branches, path); ok {
		return []string{b}, "", nil
	}
	var v struct {
		Name string `json:"name"`
	}
	found, err := r.fetch(ctx, path, &v)
	switch {
	case err != nil:
		return nil, "", err
	case !found || v.Name != b:
		// Another endpoint under the branch path (branches/main/protection) names no branch b.
		return nil, proto.ReasonProvenance, nil
	}
	keep(r.c, r.c.branches, path, struct{}{})
	return []string{b}, "", nil
}

// branchSafe is a branch that stays one name in an API path: git refuses `..`, an empty
// component and a component starting with a dot, so no real branch is refused.
func branchSafe(b string) bool {
	if strings.Contains(b, "..") {
		return false
	}
	for seg := range strings.SplitSeq(b, "/") {
		if seg == "" || strings.HasPrefix(seg, ".") {
			return false
		}
	}
	return true
}

// reaches asks compare/base...head; reached when its status is one of want, found when the
// forge answered the compare at all. Only a reach is cached.
func (r *run) reaches(ctx context.Context, base, head string, want ...string) (bool, bool, error) {
	path := r.base + "/compare/" + base + "..." + head
	key := path + "|" + strings.Join(want, ",")
	if _, ok := lookup(r.c, r.c.reach, key); ok {
		return true, true, nil
	}
	var v struct {
		Status string `json:"status"`
	}
	found, err := r.fetch(ctx, path, &v)
	if err != nil || !found {
		return false, found, err
	}
	if !slices.Contains(want, v.Status) {
		return false, true, nil
	}
	keep(r.c, r.c.reach, key, struct{}{})
	return true, true, nil
}

// file is the path form's second half: contents/PATH at the SHA is a file.
func (r *run) file(ctx context.Context) (string, error) {
	path := r.base + "/contents/" + r.need.Ref.Path + "?ref=" + r.need.Ref.SHA
	if _, ok := lookup(r.c, r.c.files, path); ok {
		return Proven, nil
	}
	var v struct {
		Type string `json:"type"`
	}
	found, err := r.fetch(ctx, path, &v)
	switch {
	case err != nil:
		return "", err
	case !found || v.Type != "file":
		return proto.ReasonUnresolved, nil
	}
	keep(r.c, r.c.files, path, struct{}{})
	return Proven, nil
}

// pull is the pr form: the head is the reference's own repository, its author is trusted
// (not judged for external), and the head is at or ahead of the SHA.
func (r *run) pull(ctx context.Context) (string, error) {
	path := r.base + "/pulls/" + strconv.Itoa(r.need.Ref.Num)
	v, cached := lookup(r.c, r.c.pulls, path)
	if !cached {
		found, err := r.fetch(ctx, path, &v)
		switch {
		case err != nil:
			return "", err
		case !found:
			return proto.ReasonUnresolved, nil
		}
	}
	switch {
	case v.Head.Repo == nil || !strings.EqualFold(v.Head.Repo.FullName, r.need.Ref.OwnerRepo()):
		return proto.ReasonProvenance, nil
	case r.need.Class != proto.RefExternal && !slices.Contains(trustedAssociations, v.Association):
		return proto.ReasonProvenance, nil
	case !shaRe.MatchString(v.Head.SHA):
		return proto.ReasonUnresolved, nil
	}
	if !cached {
		keep(r.c, r.c.pulls, path, v)
	}
	reached, found, err := r.reaches(ctx, r.need.Ref.SHA, v.Head.SHA, "identical", "ahead")
	switch {
	case err != nil:
		return "", err
	case reached:
		return Proven, nil
	case !found:
		return proto.ReasonUnresolved, nil
	}
	return proto.ReasonProvenance, nil
}

// issue is the issue form: it exists and is not a pull request.
func (r *run) issue(ctx context.Context) (string, error) {
	path := r.base + "/issues/" + strconv.Itoa(r.need.Ref.Num)
	if _, ok := lookup(r.c, r.c.issues, path); ok {
		return Proven, nil
	}
	var v map[string]json.RawMessage
	found, err := r.fetch(ctx, path, &v)
	switch {
	case err != nil:
		return "", err
	case !found:
		return proto.ReasonUnresolved, nil
	}
	if pr, isPR := v["pull_request"]; isPR && string(pr) != "null" {
		return proto.ReasonUnresolved, nil
	}
	keep(r.c, r.c.issues, path, struct{}{})
	return Proven, nil
}

// fetch decodes a 200 answer into into; found is false for an answer saying the thing is not
// there. Any other answer ends the check unresolved.
func (r *run) fetch(ctx context.Context, path string, into any) (bool, error) {
	a, err := r.get(ctx, path)
	if err != nil {
		return false, err
	}
	switch a.status {
	case http.StatusOK:
		if err := json.Unmarshal(a.body, into); err != nil {
			return false, outcome(proto.ReasonUnresolved)
		}
		return true, nil
	case http.StatusNotFound, http.StatusGone, http.StatusUnprocessableEntity:
		return false, nil
	}
	return false, outcome(proto.ReasonUnresolved)
}

type answer struct {
	status int
	header http.Header
	body   []byte
}

// get is one API GET with A 11.3's rate path and one retry after a 401 with the credential
// read again. A rate wait is slept only when it ends inside the caller's bound.
func (r *run) get(ctx context.Context, path string) (answer, error) {
	rated, retried := 0, false
	for {
		if err := r.mayAsk(); err != nil {
			return answer{}, err
		}
		tok, err := r.c.login.Token(ctx)
		if err != nil {
			return answer{}, err
		}
		a, err := r.once(ctx, path, tok)
		if err != nil {
			return answer{}, err
		}
		r.c.noteWindow(a.header)
		switch {
		case a.status == http.StatusUnauthorized:
			r.c.login.Drop()
			if retried {
				return answer{}, outcome(proto.ReasonUnresolved)
			}
			retried = true
		case rateLimited(a):
			rated++
			if err := r.waitRate(ctx, a.header, rated); err != nil {
				return answer{}, err
			}
		case a.status == http.StatusForbidden:
			return answer{}, outcome(proto.ReasonUnresolved)
		case a.status >= http.StatusInternalServerError && path == r.base:
			return answer{}, fmt.Errorf("%w: status %d", ErrDown, a.status)
		case a.status >= http.StatusInternalServerError:
			return answer{}, outcome(proto.ReasonUnresolved)
		default:
			return a, nil
		}
	}
}

// waitRate is the rate answer of try rated: it stops every check for its wait, and is slept
// unless it is the last try, over maxRateWait or past the caller's bound (forge-rate).
func (r *run) waitRate(ctx context.Context, h http.Header, rated int) error {
	wait := r.c.rateWait(h)
	r.c.markRated(wait)
	if rated == rateTries || wait > maxRateWait || r.c.passesBound(ctx, wait) {
		return outcome(proto.ReasonForgeRate)
	}
	if err := r.c.clock.Sleep(ctx, wait); err != nil {
		return fmt.Errorf("forge: the check ended: %w", err)
	}
	return nil
}

// mayAsk is a receive check's two bounds before a request (n3d-D31): the forge's rate window
// down to the reserve kept for sends, and no sender with budget left (n3d-D34), are each
// forge-rate.
func (r *run) mayAsk() error {
	switch {
	case !r.receive:
		return nil
	case r.c.inReserve():
		return outcome(proto.ReasonForgeRate)
	case r.spent:
		return nil
	}
	r.spent = true
	if !r.spend() {
		return outcome(proto.ReasonForgeRate)
	}
	return nil
}

// noteWindow keeps the rate window an answer gives; an answer without one changes nothing, and
// one without a reset is held for maxRateHold.
func (c *Checker) noteWindow(h http.Header) {
	remaining, ok := headerSeconds(h, "X-Ratelimit-Remaining")
	if !ok {
		return
	}
	limit, hasLimit := headerSeconds(h, "X-Ratelimit-Limit")
	if !hasLimit {
		limit = 0
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	resetMS := c.clock.WallMS() + maxRateHold.Milliseconds()
	if reset, ok := headerSeconds(h, "X-Ratelimit-Reset"); ok {
		resetMS = min(resetMS, reset*1000)
	}
	c.window = rateWindow{known: true, remaining: remaining, limit: limit, resetMS: resetMS}
}

// inReserve is a rate window, not yet reset, with fewer requests left than the receive reserve.
func (c *Checker) inReserve() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	w := c.window
	return w.known && c.clock.WallMS() < w.resetMS && w.remaining < proto.ForgeReceiveReserve(w.limit)
}

// once is one request, ended after requestLimit on the clock.
func (r *run) once(ctx context.Context, path string, tok Token) (answer, error) {
	tryCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	stop := r.c.clock.AfterFunc(requestLimit, cancel)
	defer stop()
	req, err := http.NewRequestWithContext(tryCtx, http.MethodGet, r.origin+path, nil)
	if err != nil {
		return answer{}, outcome(proto.ReasonUnresolved)
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-Github-Api-Version", "2022-11-28")
	req.Header.Set("User-Agent", "teamchat")
	req.Header.Set("Authorization", tok.header())
	var connected atomic.Bool
	req = req.WithContext(httptrace.WithClientTrace(tryCtx, &httptrace.ClientTrace{
		GetConn: func(string) { connected.Store(false) },
		GotConn: func(httptrace.GotConnInfo) { connected.Store(true) },
	}))
	health := path == r.base
	resp, err := r.c.http.Do(req)
	if err != nil {
		return answer{}, lost(ctx, err, health || !connected.Load())
	}
	body, readErr := io.ReadAll(io.LimitReader(resp.Body, maxBody+1))
	closeErr := resp.Body.Close()
	switch {
	case readErr != nil:
		return answer{}, lost(ctx, readErr, health)
	case len(body) > maxBody:
		return answer{}, outcome(proto.ReasonUnresolved)
	case closeErr != nil:
		return answer{}, lost(ctx, closeErr, health)
	}
	return answer{status: resp.StatusCode, header: resp.Header, body: body}, nil
}

// lost is a request with no answer: the caller's context ending, a refused redirect, or a
// forge that did not answer. Only down (no connection made, or the repository's own request
// unanswered) is evidence of the forge's health; a compare or a contents request cut short is
// shaped by the reference, so it leaves that check unresolved and nothing more (n3d-D31).
func lost(ctx context.Context, err error, down bool) error {
	switch {
	case ctx.Err() != nil:
		return fmt.Errorf("forge: the check ended: %w", ctx.Err())
	case errors.Is(err, errOffOrigin) || errors.Is(err, errRedirects) || !down:
		return outcome(proto.ReasonUnresolved)
	}
	return fmt.Errorf("%w: %w", ErrDown, err)
}

// rateLimited is A 11.3's rate answer: a 429, or a 403 that says it is a rate limit.
func rateLimited(a answer) bool {
	return a.status == http.StatusTooManyRequests ||
		a.status == http.StatusForbidden && (a.header.Get("Retry-After") != "" || a.header.Get("X-Ratelimit-Remaining") == "0")
}

// rateWait is Retry-After in seconds, else the reset time on the wall clock, else 5 s.
func (c *Checker) rateWait(h http.Header) time.Duration {
	if n, ok := headerSeconds(h, "Retry-After"); ok {
		return time.Duration(n) * time.Second
	}
	if n, ok := headerSeconds(h, "X-Ratelimit-Reset"); ok {
		return max(0, time.Duration(n*1000-c.clock.WallMS())*time.Millisecond)
	}
	return defaultRateWait
}

func headerSeconds(h http.Header, name string) (int64, bool) {
	n, err := strconv.ParseInt(h.Get(name), 10, 64)
	if err != nil || n < 0 {
		return 0, false
	}
	return min(n, maxHeaderSeconds), true
}
