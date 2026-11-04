# Unblocking everything: what needs you (updated 2026-10-30, after your answers)

## Your answers, recorded

- **A1 re-own.** Seat 2 looked first (read-only). It is the dev database of the project-A project: postgres 16 (timescale), database `project-A`, about 1 MiB of rows, last written on 13 Jan. That project's checkout still exists. Re-owning keeps it usable there. It runs after the current deploy, as a dry run first.
- **A2 default.** Recorded in Plan 00209.
- **A3 confirmed, with your condition** recorded as a direction in Plan 00209: the homeserver may live on this desktop, another server or the cloud, reached over a private network (VPN or mesh), with agents on several machines. A review is checking now that no confirmed choice rules that out (Task 0.6).
- **A7 a:** port to Go. Recorded in Plan 00191.
- **A9:** the style guide launched as version 1. Changes bump its version.
- **A6:** shelved. No CPU-only torch for now.
- **DBF:** now a hard rule in CLAUDE.md for the whole repository.
- **A3's condition, checked:** no confirmed choice rules out a remote homeserver. Four small design changes follow before the build (Plan 00209 Task 0.6).
- **No shortcut keys in ccy-teams:** built and pushed. Every action is a line of the menu Enter opens.

## Decided for you by a Fable review (2026-10-31, as you asked)

You went unattended and asked a Fable agent to pick clear winners. It picked one for every
decision below. Say so to reverse any of them.

- **A10 file:** filed upstream as hooks-daemon #145 (body from the daemon's own generator, checked for install-specific text).
- **A11 accept:** both wider checks stay unbuilt.
- **A12 no:** both questions start on No. The review found the same Yes default in two more places of the team wizard, and all are being changed together.
- **B4 bash:** the host-key fetch stays bash until the image's start script is ported whole.

## The decisions as they were put (now decided, above)

### A10. Plan 00192: the hooks-daemon guard that blocks a word, not a file (questions room Q8)

The hooks daemon's secret-file guard refuses any command or file whose text contains
NetworkManager's VPN-secrets setting name, even when no file is read. `nord` (your NordVPN
tool) works round it by building that name from its parts, and a test pins that. Upstream
documents the block as deliberate, so it is not a bug.

- **file**: file a feature request upstream, asking the guard to judge file paths, not every word. The body comes from the daemon's own report generator, so nothing private goes into the public issue. *(Recommended: costs one issue, and helps anyone else who hits it.)*
- **close**: keep the workaround and close the task.

**Say:** "A10 file" or "A10 close".

### A11. Plan 00191: two wider checks the bug fix did not build

The "could not reach the API" fix added a QA check that blocks one kind of bug in our Go
code: a command fails, and the code keeps only "it failed", not why. Two wider checks were
considered and not built. Under the repo's DBF rule, leaving them unbuilt is your call.

- **The same check for bash.** No bash linter can express it well. The bash it would guard
  is being moved to Go anyway, where the check already applies (all 8 known bash cases are
  now fixed or moved).

- **The check for any error, not only a command's.** Most such errors are deliberate ("file
  not found" means "use the default"), so the check would flag many correct lines, and each
  would need a person's judgement.

- **accept**: leave both unbuilt. *(Recommended: the bash is going, and the wider check
  would be mostly noise.)*

- **build**: say which one, and it becomes a task.

**Say:** "A11 accept" or "A11 build bash" / "A11 build any-error".

A11 is also questions room Q10; you can answer there instead.

### A12. ccy-teams: which answer is picked when you just press Enter (Plan 00201)

A12 is also questions room Q11.

Two yes/no questions in ccy-teams have Yes already picked: "Start a ccy session in
<repo>?" and, after a new password, "Open <team> in Element now?". A stray Enter then starts a
session or opens Element.

- **no**: both start on No. *(Recommended: both do something you may not want, and saying Yes
  costs one arrow key.)*
- **keep**: leave them on Yes.

**Say:** "A12 no" or "A12 keep".

## Still open: one decision (2026-10-31)

### A13. Dictation sends Enter or Ctrl+S right after a paste (issue #106)

The #106 fix stops a paste going into the wrong Ptyxis tab. While doing it, the worker found
the same kind of risk one step later. When dictation ends with "send" or "save", the
extension presses Enter or Ctrl+S about 0.3 s after the paste, and it does not check which
window has focus first. If focus moves in that 0.3 s, the key goes to the new window. Tests
from Plan 00164 pin the current behaviour, so changing it is your call, not mine.

- **Option 1 (recommended):** check focus again before pressing the key. If it moved, don't
  press it, and say so in the notification.
- **Option 2:** leave it. The window is 0.3 s and nobody has seen it happen.

**Decided by a Fable review: Option 1.** The check already exists. When focus was lost once
during a dictation, `src-go/internal/wsi/paste.go` asks the panel again before the key and
refuses with "The text was pasted but not sent". Only the usual path skips it. The fix runs
that same check on every path, and the 00164 tests change in the same commit, with the reason
stated. Deployed 2026-10-31 (ac7383246). Nothing for you to do.

### A14. Two wider QA checks the host test fix did not build (Plan 00191 Task 4.11)

Three ccy tests failed only on your desktop, because its locale, PATH and an exported
`which` function reached them. Every QA command now runs with a fixed locale, no exported
shell functions, and a PATH without this repo's installed commands, and a blocking test
guards that. The review left two wider versions unbuilt. Under the Defence Before Fix rule,
leaving them out is your call:

- **Option 1 (recommended): leave both.** The runtime guard already blocks.
  - A static analyzer would only re-prove the same seam.
  - HOME has to stay real, because Go's and semgrep's caches live there, and the suites
    already set their own scratch HOME.
- **Option 2:** build the static analyzer as well.

**Decided by a Fable review: Option 1.** The machine environment is already blocked at the one
seam every QA command passes through. A static analyzer would only re-detect the same class.

### A15. arm64: the known x86-only downloads on a ratchet list (Plan 00243)

The new QA rule `arch-pinned-artefact` catches any download, package or image fixed to one
CPU architecture. The two ccy images are fixed. 44 other instances in 21 files are not yet
fixed: CI, the Fedora install scripts, kernel locking, docker-in-lxc, the VM lab and 11
playbooks. They are listed in `scripts/arch-pin.allowlist`. A listed file may only lose
findings, so any new x86-only download is blocked now. Under the Defence Before Fix rule,
accepting a baseline like this is your decision.

- **Option 1 (recommended): accept the list while Plan 00243 works through it.** The rule
  becomes plainly blocking once the list is empty. All of this is on the `F44-arm` branch
  only.
- **Option 2:** fix all 44 before the rule goes in at all. Nothing guards against new ones
  until then.

**Decided by a Fable review: Option 1.** This is the repo's existing ratchet pattern (like the
bash-limits allowlist), and Option 2 would leave the class unguarded while 44 fixes land. One
caveat for you: play-nvidia's 5 entries cannot be fixed by choosing a build, because there is
no CUDA toolkit for Fedora aarch64. They must end as a clear "no build for this architecture"
failure, not as permanent rows on the list.

**Update, 2026-11-02: the list is empty.** All 44 rows were fixed on `F44-arm`, and play-nvidia now
refuses CUDA on aarch64 by name. The gate refuses any new row, so the rule now blocks every
x86-only download. Nothing for you to do.

## Still open: two things only you can unblock (2026-11-02)

### A16. Run the deploy queue from a terminal (Plans 00177, 00234, 00192)

Seat 2's last deploy run failed before any play started: from an agent shell, run.bash ran
unattended and refused. run.bash 1.36.2 (on `F44` now) should let seat 2 run it itself, and
I have asked seat 2 to try. If it still cannot, it needs you: run
`./CLAUDE/Plan/meta-deploy.bash` in a terminal. It runs the changed plays (play-python,
play-claude-yolo). After that, seat 2 can do the host checks for 00177 and 00192, and you can
do A4d for 00234.

**Say:** nothing, if seat 2 reports the deploy ran; otherwise "A16 done" after you run it.

### A17. Seat 2's fix to go-qa-ci (Plan 00191)

One Go QA check (deadcode) fails only on your desktop. go-qa-ci names a tool folder after the
Go version, and Fedora's Go version contains a `:`, which breaks PATH. Seat 2 has written the
fix in a local clone of LongTermSupport/go-qa-ci. Its permission settings refused running that
repo's tests and pushing a PR, so it asked you. I won't do it in its place.

- **yes:** seat 2 runs the tests, opens the PR, and I bump the pin here once it merges.
- **no:** the host's full QA keeps failing at deadcode; the container's still passes.

**Say:** "A17 yes" or "A17 no".

**Answered 2026-11-02: yes**, in seat 2's session. The fix is go-qa-ci PR #1; I reviewed the
diff and it is right. Left for you: merge it and tag a release (agents never tag), then I bump
the pin here. Its "Public safety" check fails on main too, not because of the PR: the
go-qa-ci repository secret `GQACI_DENYLIST` is not set.

## Your answers, 2026-11-02

- **v2: push it and get it done (option 2, the minimum v2 with cross-network teams).** v1 takes
  no new features. A first measuring merge of the v2 branch onto F44 is on
  `spike/v2-merge-up` (f5023d692): it builds and vets clean, and the merged project rules
  list 29 findings (24 in v2's code, 5 in F44 code from one of v2's rules). Those, plus
  merging four docs by meaning, are the first work list. Nothing is merged into F44 yet.
- **No branch reaches the full CI suite without F44 in it.** A new first CI job fails such a
  push in seconds and the full suite waits on it (`ci/require-default-base`).

## Still open: A4, hands-on checks (no decisions)

Each one waits for a moment when you are doing that thing anyway. None blocks other work.

### A4h. No pipx warnings on a re-run (Plan 00188, issue #54)

The next time you run a plain `./run.bash`, check that no ⚠️ line from pipx appears
("already injected"). The fix is in run.bash 1.32.1. The deploy queue never runs this step,
so only your own run can show it.

### A4b. Adding an LXC container to an agent team (Plan 00177)

**Nothing for you to do** on containers that are already set up. The join checks first: it
installs gh only where gh is missing, and asks for a login only for an agent user whose gh
is not logged in to GitHub (pingbus uses that login). Seat 2 can run the remaining test with
a real, already set-up container, at a time you are around for its sudo prompt.

### A4c. Copying your Element password from ccy-teams (Plan 00201)

**What:** in `ccy-teams`, Enter on your own row under a team, then **New password**, gives you a new Element
password for that team. Check once, for real, that the copy behaves safely.

**Note:** this changes your Element password for that team and signs your Element sessions
out, so do it when you are about to sign in anyway.

1. With Clipboard Indicator on (the default), choose New password and answer Yes. It should **not** offer a copy; it shows the password to type instead. The top-bar clipboard menu must not list it.
2. Switch Clipboard Indicator off (Extensions app), choose New password again and pick "Yes, copy the password". Paste it into Element's sign-in and sign in.
3. A second paste anywhere should paste nothing. Then switch Clipboard Indicator back on.

### A4d. The speech-to-text model window (Plan 00234)

**When:** at your next log-out and log-in (the play has run).

**What:** open the speech-to-text menu in the top bar and choose **Manage Whisper Models...**.
Check two things: the line "Checking the GPU and the models..." appears straight away, and
the GPU line is easy to read.

### A4e. The camera hotspot password (Plan 00192 Task 3.16)

**When:** next time you use the camera with the laptop's hotspot.

**What:** run `ftp-camera --hotspot`. The camera settings it shows should list
`Wi-Fi: ftp-camera` and the password the play generated (three words). Check that the camera
joins with what is shown.

### A4f. ccy's new launch checks and token bars (Plan 00191)

**When:** your next normal ccy launch (the host is on ccy 3.104.11).

**What:** just launch as usual. It should behave as before. If something fails, the message
should now say what actually went wrong. Also, when you next see them:

- `ccy --token-usage` draws coloured bars, not a table.
- A token with no usage left shows a red banner before the token chooser.

### A4g. Tools moved from bash to Go, checks only you can do (Plan 00192 Task 3.3)

Each tool already passed every check the host agent can run. These need your screen, your
speakers, a real account or the camera. Do them when you next use the tool anyway; each spec
in the plan folder has the exact steps.

- `open`: opening a file from the desktop (H2, H3), and Ctrl-C while a video plays in mpv.
- `gh-account-setup`: a real GitHub device-code login for an account (H2), and Ctrl-C in the
  middle of one (H5), which must leave your `gh` accounts as they were.
- `setup-rclone`: H1-H4, with a real remote and the vault.
- `wsi`: a live dictation and its audible grace period (H1).
- `ftp-camera`: the camera uploading a frame (H3, H4), and H1/H5.
- `vmtest` H6: needs a throwaway GitHub account. Say "skip H6" if you'd rather not make one.

## For information: one departure from your A7 answer

All 8 "guess instead of the cause" places in ccy are fixed. Seven now run in Go, behind the
rule that blocks the bug. The eighth, the GitHub host-key fetch when a ccy container starts,
runs inside the container image, which has no Go program of ours, so it was fixed in the
image's start script instead (the script got smaller, not bigger). It now says what failed
(DNS, connection, a rate limit) instead of "offline?", and the container still starts.
Say "B4 Go" if you want a Go program built into the image for it.
