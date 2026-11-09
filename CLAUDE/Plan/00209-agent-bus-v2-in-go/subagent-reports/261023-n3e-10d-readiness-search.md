**Summary.** I found one instance in first-party Go that carries the hazard: `src-go/internal/teamchat/member/link/parent.go:62`, the originating one. No second instance exists. Searches covered `src-go/` and the nested `src-go/qa`, production and test code, with `vendor/` and `testdata/` excluded and the keep-out paths unread. Of the four readiness waits through the poller, three are `os.File.Read` loops, which the runtime tests by actually reading. The fourth, `keeper/osproc.go:92`, is the only other `RawConn.Read` callback in first-party Go. It tests the descriptor on every call, so it does not carry the hazard. I wrote no report file, because your instruction was to change no file. Nothing else was changed.

## Instances

| Location | What it does | Tests real fd state on every call? | Verdict |
|---|---|---|---|
| `internal/teamchat/member/link/parent.go:62-66` in `watch()` | `rc.Read(func(uintptr) bool { was := armed; armed = true; return was })` on a `PIDFD_NONBLOCK` pidfd. The fd argument is ignored. It returns false on the first call and true afterwards. | No | **CARRIES THE HAZARD.** `RawConn.Read` resets readiness before the first callback. A pidfd whose process already exited then reports "not ready" untested, and the wait never ends. |
| `internal/teamchat/member/keeper/osproc.go:92-99` in `pidfd.wait()` | `rc.Read(func(fd uintptr) bool {...})` runs `unix.Poll([]PollFd{{fd, POLLIN}}, 0)` on every call. It returns true if the poll errors or reports n > 0, and true if fd exceeds MaxInt32. | Yes, on every call | **DOES NOT.** A zero-timeout poll sees an already-exited process, so the reset readiness is not lost. |

## Other routes to readiness, all checked and cleared

- `keeper/osproc.go:154-179`, `inotify.read`: blocking `os.File.Read` on a non-blocking inotify fd. The runtime reads first and parks only on EAGAIN. **DOES NOT.**
- `proof/watch.go:91` and `teamsync/watch.go:62`: the same `w.f.Read(buf)` loop on an inotify file. **DOES NOT.**
- `RawConn.Control` calls make no readiness decision. **DOES NOT** apply; they are listed so a rule can skip them:
  - `keeper/osproc.go:73` and `:134`
  - `link/server.go:311`
  - `wire/control.go:480`, `:512` and `:616`
  - `proof/watch.go:122`
- `guest/check.go:211-231` opens a pidfd and an inotify instance only to prove the kernel supports them, then closes them. No wait. **DOES NOT.**
- `execx/session.go:264` calls `SetReadDeadline`. It is a deadline, not a readiness callback. **DOES NOT.**
- No first-party hand-rolled epoll, select, eventfd or `unix.Poll` loop exists apart from `osproc.go:97`. The epoll, select and `Eventfd` hits are all under `vendor/` (ultraviolet, cancelreader, x/sys).
- I only listed these files for raw syscalls and did not read them: `internal/teamchat/seat/onecopy_test.go`, `internal/teamchat/seat/planted_test.go` and `qa/internal/cmdmain/cmdmain.go`. Nothing in them looks like a readiness wait.

## Searches run

- `SyscallConn`, `RawConn`, `.Control(`
- `rc.Read(func`, `raw.Read`, `conn.Read(func`, `func(fd uintptr) bool`, `func(uintptr) bool`
- `epoll`, `EpollWait`, `unix.Poll`, `Ppoll`, `Select`, `Eventfd`
- `PidfdOpen`, `pidfd`, `inotify`
- `SetNonblock`, `SetReadDeadline`, `unix.Read(`, `unix.Syscall`
- A repo-wide `SyscallConn` check outside `src-go` found none.

I looked for these forms:
- a `RawConn.Read` or `Write` callback that ignores its fd argument or uses an armed flag
- a callback that returns a constant
- hand-rolled epoll
- a poll or select loop
- a pidfd, inotify or eventfd wait

## Attribution by technique

**Text search found:**
- every `SyscallConn` site (9)
- the only two `rc.Read` callbacks, `parent.go:62` and `osproc.go:92`
- the absence of any other epoll, select or poll in first-party code

**Reading alone could judge:**
- that `parent.go:62` ignores its fd and uses an armed flag, so it never tests state
- that `osproc.go:92` polls the real fd with timeout 0 on every call
- that the `os.File.Read` loops and `Control` sites make no readiness decision of their own
- that the guest check only opens and closes descriptors

Grep could not check these: a callback's semantics, whether it ignores its argument, and whether a `Read` loop depends on the poller. Text search also cannot show that no other shape exists, such as a wait wrapped behind a helper or interface. I covered that by reading every `SyscallConn` caller, and the grep for raw syscalls found nothing more.