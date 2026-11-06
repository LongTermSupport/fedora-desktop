package fakehs

import (
	"context"
	"strings"
	"sync"
)

// gateQueue is how many held requests and permits a gate keeps without blocking either side.
const gateQueue = 4096

// Gate holds the requests it matches before the server handles them, each until the test lets
// one go (Pass) or its caller goes, so a test can order a request against what it does
// meanwhile, as a slow homeserver would; a fault answers a request, a gate decides when. It
// matches as a Fault does: the method exactly, the path a substring of the percent-encoded path.
type Gate struct {
	method, path string
	arrived      chan struct{}
	permits      chan struct{}
	once         sync.Once
	opened       chan struct{}
}

// Gate holds every request of method whose path holds path, from now on, until Open.
func (s *Server) Gate(method, path string) *Gate {
	g := &Gate{
		method: method, path: path, arrived: make(chan struct{}, gateQueue), permits: make(chan struct{}, gateQueue),
		opened: make(chan struct{}),
	}
	s.gateMu.Lock()
	defer s.gateMu.Unlock()
	s.gates = append(s.gates, g)
	return g
}

// Held returns once a request the gate holds has arrived, one per call.
func (g *Gate) Held(ctx context.Context) error {
	select {
	case <-g.arrived:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Arrived delivers one value per request the gate holds, as Held does, for a select.
func (g *Gate) Arrived() <-chan struct{} { return g.arrived }

// Pass lets one held request go: the oldest waiting, or the next to arrive.
func (g *Gate) Pass() { g.permits <- struct{}{} }

// Open lets every request go, held or later.
func (g *Gate) Open() { g.once.Do(func() { close(g.opened) }) }

// gated is a request's wait at the server's gates; false when its caller went first.
func (s *Server) gated(ctx context.Context, method, path string) bool {
	s.gateMu.Lock()
	var held []*Gate
	for _, g := range s.gates {
		if g.method == method && strings.Contains(path, g.path) {
			held = append(held, g)
		}
	}
	s.gateMu.Unlock()
	for _, g := range held {
		select {
		case <-g.opened:
			continue
		default:
		}
		g.arrived <- struct{}{}
		select {
		case <-g.permits:
		case <-g.opened:
		case <-ctx.Done():
			return false
		}
	}
	return true
}
