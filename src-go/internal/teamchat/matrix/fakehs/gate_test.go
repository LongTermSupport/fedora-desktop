package fakehs

import (
	"context"
	"net/http"
	"net/url"
	"testing"
)

// ask makes one backward /messages of the team's room as its agent, under ctx, and sends its
// status to out.
func (tm *team) ask(ctx context.Context, out chan<- int) {
	status, body := tm.s.Handle(ctx, http.MethodGet, tm.roomPath("messages"), url.Values{"dir": {"b"}}, nil, tm.tok[tm.agent])
	if status == http.StatusOK && len(body) == 0 {
		status = -1
	}
	out <- status
}

// A gate holds each request it matches before the server handles it, until the test lets it go,
// one at a time; a request it does not match is not held, and an opened gate holds nothing.
func TestAGateHoldsTheRequestsItMatchesUntilEachIsLetGo(t *testing.T) {
	tm := newTeam(t)
	g := tm.s.Gate(http.MethodGet, "/messages")
	answered := make(chan int, 2)
	go tm.ask(t.Context(), answered)
	if err := g.Held(t.Context()); err != nil {
		t.Fatal(err)
	}
	if got := tm.ok(tm.whoami(tm.agent)); got["user_id"] != tm.agent {
		t.Fatalf("a request the gate does not match was held: %v", got)
	}
	select {
	case s := <-answered:
		t.Fatalf("the held request was answered %d before it was let go", s)
	default:
	}
	g.Pass()
	if s := <-answered; s != http.StatusOK {
		t.Fatalf("the request let go was answered %d", s)
	}
	g.Open()
	go tm.ask(t.Context(), answered)
	if s := <-answered; s != http.StatusOK {
		t.Fatalf("a request through an opened gate was answered %d", s)
	}
}

// A held request whose caller goes is not answered, and the gate holds nothing past it.
func TestAHeldRequestEndsWithItsCaller(t *testing.T) {
	tm := newTeam(t)
	g := tm.s.Gate(http.MethodGet, "/messages")
	ctx, cancel := context.WithCancel(t.Context())
	answered := make(chan int, 1)
	go tm.ask(ctx, answered)
	select {
	case <-g.Arrived():
	case <-t.Context().Done():
		t.Fatal("the request was never held")
	}
	cancel()
	if s := <-answered; s != 0 {
		t.Fatalf("a request whose caller went was answered %d", s)
	}
}
