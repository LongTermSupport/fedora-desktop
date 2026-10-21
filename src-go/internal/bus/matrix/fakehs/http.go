package fakehs

import (
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	// maxHTTPBody is the most of a request body the HTTP side reads; past it the answer is 413.
	maxHTTPBody       = 16 << 20
	readHeaderTimeout = 10 * time.Second
)

// Request is one HTTP request as it reached the server: the method, the path as on the wire,
// the query and the headers. The body is never kept (SPEC M42).
type Request struct {
	Method string
	Path   string
	Query  url.Values
	Header http.Header
}

type webState struct {
	mu       sync.Mutex
	addr     string
	srv      *http.Server
	done     chan error
	closing  bool
	active   sync.WaitGroup
	requests []Request
}

// Start serves the server over HTTP on 127.0.0.1: at a free port the first time, and at the
// same address after Stop.
func (s *Server) Start() error {
	w := &s.web
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.srv != nil {
		return errors.New("fakehs: already serving")
	}
	addr := w.addr
	if addr == "" {
		addr = "127.0.0.1:0"
	}
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("fakehs: listen: %w", err)
	}
	w.addr = ln.Addr().String()
	w.srv = &http.Server{Handler: http.HandlerFunc(s.serveHTTP), ReadHeaderTimeout: readHeaderTimeout}
	w.done = make(chan error, 1)
	w.closing = false
	go func(srv *http.Server, done chan<- error) {
		err := srv.Serve(ln)
		if errors.Is(err, http.ErrServerClosed) {
			err = nil
		}
		done <- err
	}(w.srv, w.done)
	return nil
}

// URL is the base URL Start serves at.
func (s *Server) URL() string {
	s.web.mu.Lock()
	defer s.web.mu.Unlock()
	return "http://" + s.web.addr
}

// Stop closes the listener and every open connection, and waits for the requests in flight to
// end; connections are refused until Start or Restart. Stopping a server that is not serving
// does nothing.
func (s *Server) Stop() error {
	w := &s.web
	w.mu.Lock()
	srv, done := w.srv, w.done
	w.srv, w.closing = nil, true
	w.mu.Unlock()
	if srv == nil {
		return nil
	}
	closeErr := srv.Close()
	w.active.Wait()
	if err := <-done; err != nil {
		return fmt.Errorf("fakehs: serve: %w", err)
	}
	if closeErr != nil {
		return fmt.Errorf("fakehs: close: %w", closeErr)
	}
	return nil
}

// Restart drops every open connection (a held sync ends mid-poll), keeps the state, empties
// the login-failure counts and serves again at the same address.
func (s *Server) Restart() error {
	if err := s.Stop(); err != nil {
		return err
	}
	s.resetLoginFailures()
	return s.Start()
}

// Close stops the server; it is Stop, for a test's cleanup.
func (s *Server) Close() error { return s.Stop() }

// Requests are the HTTP requests received so far, in order.
func (s *Server) Requests() []Request {
	s.web.mu.Lock()
	defer s.web.mu.Unlock()
	return append([]Request(nil), s.web.requests...)
}

// enter records r and counts it in flight, unless the server is stopping.
func (w *webState) enter(r *http.Request, path string) bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closing {
		return false
	}
	w.active.Add(1)
	w.requests = append(w.requests, Request{Method: r.Method, Path: path, Query: r.URL.Query(), Header: r.Header.Clone()})
	return true
}

func wirePath(r *http.Request) string {
	if i := strings.IndexByte(r.RequestURI, '?'); i >= 0 {
		return r.RequestURI[:i]
	}
	return r.RequestURI
}

func bearer(header string) string {
	token, found := strings.CutPrefix(header, "Bearer ")
	if !found {
		return ""
	}
	return token
}

// drop closes the connection under w without an answer.
func drop(w http.ResponseWriter) {
	hj, ok := w.(http.Hijacker)
	if !ok {
		panic("fakehs: the HTTP server cannot drop a connection")
	}
	conn, rw, err := hj.Hijack()
	if err != nil {
		return
	}
	rw.Writer.Reset(io.Discard)
	if err := conn.Close(); err != nil {
		return
	}
}

func (s *Server) serveHTTP(w http.ResponseWriter, r *http.Request) {
	path := wirePath(r)
	if !s.web.enter(r, path) {
		drop(w)
		return
	}
	defer s.web.active.Done()
	body, err := io.ReadAll(io.LimitReader(r.Body, maxHTTPBody+1))
	if err != nil {
		drop(w)
		return
	}
	if len(body) > maxHTTPBody {
		write(w, errorOutcome(&apiError{status: http.StatusRequestEntityTooLarge, errcode: "M_TOO_LARGE", text: "Request body too large."}))
		return
	}
	out := s.process(r.Context(), r.Method, path, r.URL.Query(), body, bearer(r.Header.Get("Authorization")))
	if out.fault == nil {
		write(w, out)
		return
	}
	switch {
	case out.fault.Drop, out.fault.DropAfter:
		drop(w)
		return
	case out.fault.Redirect != "":
		w.Header().Set("Location", out.fault.Redirect)
		if out.status == 0 {
			out.status = http.StatusTemporaryRedirect
		}
	}
	for key, values := range out.fault.Header {
		for _, v := range values {
			w.Header().Add(key, v)
		}
	}
	write(w, out)
}

func write(w http.ResponseWriter, out outcome) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Length", strconv.Itoa(len(out.body)))
	w.WriteHeader(out.status)
	if _, err := w.Write(out.body); err != nil {
		return // the client has gone; there is no one to tell
	}
}
