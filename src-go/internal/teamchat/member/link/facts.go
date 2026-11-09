package link

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"slices"
	"strconv"
	"strings"
	"sync"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/clock"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/engine"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/exits"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/homefs"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/state"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/wire"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
)

// maxChain is how many parent steps LN7's walk takes from the peer to the session process.
const maxChain = 64

const (
	msgNotStarted exits.Template = "the session has not started: no hook is served yet"
	msgChain      exits.Template = "the caller does not descend from the session's process"
	msgSocket     exits.Template = "the inbox socket is not a socket of the session's processes"
	msgOneClaude  exits.Template = "teamchat serves one Claude Code per run; this session's teams ended with the first"
	msgTranscript exits.Template = "the transcript is not the session's own transcript"
	msgSessionID  exits.Template = "the session id is not the session's: {}"
	msgSource     exits.Template = "the session source is not startup, resume, clear or compact"
	msgFacts      exits.Template = "the session's facts could not be written"
)

func refuseFacts(code string, t exits.Template, fills ...exits.Fill) *exits.Error {
	return exits.New(code, nil, t, fills...)
}

// Facts are what a hook op tells the link of the session (LN7): the op, the connecting
// process (SO_PEERCRED), and the hook's session id, source, transcript and inbox socket.
// Source is empty on an op that carries none.
type Facts struct {
	Op         string
	Peer       int
	SessionID  string
	Source     string
	Transcript string
	Socket     string
}

// sessionDeps are what the session's facts are judged and kept with.
type sessionDeps struct {
	runRoot, stateRoot *homefs.Root
	layout             paths.Layout
	run                paths.RunID
	proc               Proc
	clock              clock.Clock
	// runtimeDir is $XDG_RUNTIME_DIR, the inbox sockets' directory.
	runtimeDir string
	// claudeDir is the launch's claude_config_dir, else ~/.claude.
	claudeDir string
	// cwd is run's working directory, which names the transcript's project.
	cwd       string
	onClaude  func(state.Process)
	onStarted func()
	log       io.Writer
}

// Session is session.json's one writer and the judge of a hook's facts (LN7, B 2.4.4).
type Session struct {
	d          sessionDeps
	mu         sync.Mutex
	file       state.Session
	sessionPID *state.Process
	claude     *state.Process
	// served is whether this link has accepted facts: a new link's first op may carry a
	// session id the old link never saw.
	served  bool
	started bool
	// pidKnown is closed once the keeper has told the session's pid.
	pidKnown chan struct{}
}

// newSession reads session.json back. One that is missing is a session not yet started; one
// that does not decode is logged and started afresh, losing only what the next hook re-tells.
func newSession(d sessionDeps) (*Session, error) {
	s := &Session{d: d, file: state.Session{Run: d.run, Told: state.Told{Roles: map[paths.TeamID]string{}}}, pidKnown: make(chan struct{})}
	f, err := state.Read(d.runRoot, d.layout.Run(d.run).Session(), state.MaxSession, state.DecodeSession)
	switch {
	case err == nil:
		s.file, s.claude, s.started = f, f.Claude, f.Started()
	case errors.Is(err, fs.ErrNotExist):
	case errors.As(err, new(*state.InvalidError)):
		if _, logErr := fmt.Fprintf(d.log, "link: session.json does not decode, so it starts afresh: %v\n", err); logErr != nil {
			return nil, logErr
		}
	default:
		return nil, fmt.Errorf("link: read session.json: %w", err)
	}
	return s, nil
}

// SetSessionPID is the keeper's pid: the session process the chain must reach.
func (s *Session) SetSessionPID(p state.Process) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.sessionPID == nil {
		close(s.pidKnown)
	}
	s.sessionPID = &p
}

// AwaitPID waits until the keeper has told the session's pid, false when ctx ends first: a
// hook op before it is not answered (its hook gives its own recovery line).
func (s *Session) AwaitPID(ctx context.Context) bool {
	select {
	case <-s.pidKnown:
		return true
	case <-ctx.Done():
		return false
	}
}

// File is session.json as it is now; its maps are the caller's to read, never to change.
func (s *Session) File() state.Session {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.file
}

// Update writes session.json with edit applied, when edit says it changed anything; a write
// that fails changes nothing. edit replaces a map rather than changing it.
func (s *Session) Update(edit func(*state.Session) bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	next := s.file
	if !edit(&next) {
		return nil
	}
	return s.writeLocked(next)
}

// CountUnusable counts one hook payload that was not usable (B 2.8) in session.json, and is the
// counts written.
func (s *Session) CountUnusable() (state.Unusable, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	next := s.file
	next.Unusable.HookStdin++
	if err := s.writeLocked(next); err != nil {
		return s.file.Unusable, err
	}
	return next.Unusable, nil
}

// SetClaude is the keeper's claude: Claude Code's process, fixed for the run.
func (s *Session) SetClaude(p state.Process) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.claude = &p
}

// EndReason is session.json's end_reason, for the ends.
func (s *Session) EndReason() *string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.file.EndReason
}

// Unusable is session.json's counts of unusable inputs.
func (s *Session) Unusable() state.Unusable {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.file.Unusable
}

// Inbox is the inbox socket and transcript session.json holds, each empty before the first
// accepted facts, as the engine takes them.
func (s *Session) Inbox() engine.SessionFacts {
	s.mu.Lock()
	defer s.mu.Unlock()
	var f engine.SessionFacts
	if s.file.Socket != nil {
		f.Socket = *s.file.Socket
	}
	if s.file.Transcript != nil {
		f.Transcript = *s.file.Transcript
	}
	return f
}

// RecordRender writes session.json with unusable.render n (DV2), the rest unchanged.
func (s *Session) RecordRender(n int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	next := s.file
	next.Unusable.Render = n
	data, err := next.Encode()
	if err == nil {
		err = s.d.runRoot.Replace(s.d.layout.Run(s.d.run).Session(), data, 0o600)
	}
	if err != nil {
		return err
	}
	s.file = next
	return nil
}

// AcceptFacts judges a hook's facts and keeps them (LN7). The peer must descend from the
// session's process, by pid and start time, within 64 steps; the socket must be the inbox
// socket of a process on that chain at or below the session's, and the first fixes Claude
// Code's pid (told to the keeper); the transcript must be the session's own, through no link;
// a new session id is taken only on a session-start that clears or resumes, or as the first
// facts this link accepts. session.json is written before it returns, and the conversation
// record whenever the session id changes; the first accepted session-start tells the start.
// A refusal is an *exits.Error and changes nothing.
func (s *Session) AcceptFacts(f Facts) error { return s.acceptWith(f, nil) }

// writeLocked writes next as session.json and keeps it; a write that fails keeps the old.
func (s *Session) writeLocked(next state.Session) error {
	data, err := next.Encode()
	if err == nil {
		err = s.d.runRoot.Replace(s.d.layout.Run(s.d.run).Session(), data, 0o600)
	}
	if err != nil {
		return err
	}
	s.file = next
	return nil
}

// acceptWith is AcceptFacts with edit, when set, applied to session.json in the same write.
func (s *Session) acceptWith(f Facts, edit func(*state.Session)) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.sessionPID == nil {
		return refuseFacts(exits.CodeRuntime, msgNotStarted)
	}
	chain, err := s.chain(f.Peer)
	if err != nil {
		return err
	}
	claude, err := s.socketProcess(f.Socket, chain)
	if err != nil {
		return err
	}
	if s.claude != nil && *s.claude != claude {
		return refuseFacts(exits.CodeClaude, msgOneClaude)
	}
	if refused := s.checkTranscript(f); refused != nil {
		return refused
	}
	next, err := s.next(f, claude)
	if err != nil {
		return err
	}
	if edit != nil {
		edit(&next)
	}
	return s.keep(f, next, claude)
}

// chain is the peer and its parents up to the session's process, which must be found, with
// its start time, within maxChain steps.
func (s *Session) chain(peer int) ([]int, error) {
	pid, chain := peer, []int{peer}
	for step := 0; ; step++ {
		if pid == s.sessionPID.PID {
			ticks, err := s.d.proc.StartTicks(pid)
			if err != nil || ticks != s.sessionPID.StartTicks {
				return nil, refuseFacts(exits.CodeRuntime, msgChain)
			}
			return chain, nil
		}
		parent, err := s.d.proc.PPid(pid)
		if step == maxChain || err != nil || parent <= 0 {
			return nil, refuseFacts(exits.CodeRuntime, msgChain)
		}
		pid = parent
		chain = append(chain, pid)
	}
}

// socketProcess is the process whose inbox socket path is: paths.InboxSocket of a pid on the
// chain, with its start time.
func (s *Session) socketProcess(path string, chain []int) (state.Process, error) {
	name, ok := strings.CutPrefix(path, s.d.runtimeDir+"/cc-socks/")
	if ok {
		name, ok = strings.CutSuffix(name, ".sock")
	}
	pid, err := strconv.Atoi(name)
	if !ok || err != nil || !slices.Contains(chain, pid) {
		return state.Process{}, refuseFacts(exits.CodeRuntime, msgSocket)
	}
	if want, wantErr := paths.InboxSocket(s.d.runtimeDir, pid); wantErr != nil || want != path {
		return state.Process{}, refuseFacts(exits.CodeRuntime, msgSocket)
	}
	ticks, err := s.d.proc.StartTicks(pid)
	if err != nil {
		return state.Process{}, refuseFacts(exits.CodeRuntime, msgChain)
	}
	return state.Process{PID: pid, StartTicks: ticks}, nil
}

// checkTranscript holds the transcript to <claudeDir>/projects/<cwd, every character not a
// letter or digit made "->/<session id>.jsonl, with no link below the configuration directory.
func (s *Session) checkTranscript(f Facts) error {
	project := s.d.claudeDir + "/projects"
	dir := project + "/" + derived(s.d.cwd)
	want := dir + "/" + f.SessionID + ".jsonl"
	if f.Transcript != want {
		return refuseFacts(exits.CodeRuntime, msgTranscript)
	}
	for _, p := range []string{project, dir, want} {
		info, err := os.Lstat(p)
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		if err != nil || info.Mode()&fs.ModeSymlink != 0 {
			return refuseFacts(exits.CodeRuntime, msgTranscript)
		}
	}
	return nil
}

// derived is Claude Code's project directory name for cwd.
func derived(cwd string) string {
	b := []byte(cwd)
	for i, c := range b {
		if (c < 'a' || c > 'z') && (c < 'A' || c > 'Z') && (c < '0' || c > '9') {
			b[i] = '-'
		}
	}
	return string(b)
}

// next is session.json with the facts taken, the id swap judged.
func (s *Session) next(f Facts, claude state.Process) (state.Session, error) {
	source := f.Source
	if f.Op != wire.OpHookSessionStart {
		source = state.SourceStartup
		if s.file.Source != nil {
			source = *s.file.Source
		}
	}
	if !slices.Contains([]string{state.SourceStartup, state.SourceResume, state.SourceClear, state.SourceCompact}, source) {
		return state.Session{}, refuseFacts(exits.CodeRuntime, msgSource)
	}
	swap := f.Op == wire.OpHookSessionStart && (source == state.SourceClear || source == state.SourceResume)
	if s.file.SessionID != nil && *s.file.SessionID != f.SessionID && s.served && !swap {
		return state.Session{}, refuseFacts(exits.CodeRuntime, msgSessionID, exits.Words("a new id without a clear or resume"))
	}
	next := s.file
	next.Run = s.d.run
	next.SessionID, next.Source, next.Transcript, next.Socket = &f.SessionID, &source, &f.Transcript, &f.Socket
	next.Claude = &claude
	if _, err := next.Encode(); err != nil {
		return state.Session{}, refuseFacts(exits.CodeRuntime, msgSessionID, exits.Words("not a session id"))
	}
	return next, nil
}

// keep writes session.json and, on a new session id, the conversation record; then tells the
// keeper Claude Code's process and the start, each once.
func (s *Session) keep(f Facts, next state.Session, claude state.Process) error {
	changed := s.file.SessionID == nil || *s.file.SessionID != f.SessionID
	data, err := next.Encode()
	if err == nil {
		err = s.d.runRoot.Replace(s.d.layout.Run(s.d.run).Session(), data, 0o600)
	}
	if err == nil && changed {
		err = s.writeConversation(next)
	}
	if err != nil {
		if _, logErr := fmt.Fprintf(s.d.log, "link: keep the session's facts: %v\n", err); logErr != nil {
			err = errors.Join(err, logErr)
		}
		return errors.Join(refuseFacts(exits.CodeInternal, msgFacts), err)
	}
	s.file, s.served = next, true
	if s.claude == nil {
		s.claude = &claude
		s.d.onClaude(claude)
	}
	if f.Op == wire.OpHookSessionStart && !s.started {
		s.started = true
		s.d.onStarted()
	}
	return nil
}

func (s *Session) writeConversation(next state.Session) error {
	c := state.Conversation{Run: s.d.run, SessionID: *next.SessionID, Source: *next.Source, WrittenMS: s.d.clock.WallMS()}
	data, err := c.Encode()
	if err != nil {
		return err
	}
	return s.d.stateRoot.Replace(s.d.layout.ConversationFile(), data, 0o600)
}
