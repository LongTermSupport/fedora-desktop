# Search: terminal left cooked and echoing between screens

Dispatch contained the class and hazard description only; no rule or pattern was given. I opened
no detector, rule or QA config directory and ran no detector. src-go/qa/rules was not entered.
Paths are relative to the worktree root.

## Forms expected before searching

- Go: ui.Picker Pick and Choose (fzf), ui.Gum Confirm, Input, Choose, Spin (gum),
  pathinput.Asker.Ask (the one in-tree Bubble Tea program), ui.Lines (cooked line reads),
  tmuxccy.Attach, execx.Foreground, Exec (process replacement), sudo -v, /dev/tty readers.
- Bash and Python: fzf, gum, whiptail, dialog, select in a loop, read -p, input().
- Framework variants: tea.NewProgram, MakeRaw, termios, stty.

## Facts that hold for every finding

- No first-party code sets termios. Searching for stty, ECHO, termios, MakeRaw, term.MakeRaw,
  term.GetState and term.Restore under src-go/cmd and src-go/internal found nothing outside vendor/.
  Only the vendored Charm libraries do it, inside Bubble Tea. Nothing owns the mode between screens.
- Every Go screen is a child process (fzf, gum) or a Bubble Tea program that takes the tty raw and
  restores it on exit. Between them the tty is cooked with ECHO on. execx.OS Run and Foreground
  (src-go/internal/execx/exec.go, foreground.go) never touch modes.
- Built-in gap before every screen: ui.Gum.headingWidth (src-go/internal/ui/heading.go:292) and
  ui.Picker.headingWidth (heading.go:295-297) run tput cols as a subprocess (columns, in
  src-go/internal/ui/gumx.go) before each Confirm, Input, Choose, Pick (ui/ui.go:66,85,97;
  ui/picker.go:119; ui/choose.go:33). So even back-to-back screens have a small subprocess gap
  with echo on. A row's menu is itself a second Pick (ui/picker.go:364), directly after the first.

## A. Instances that carry the hazard

"Sure" means the code shows screen, work, screen in one process. "Likely" means the gap is short
or depends on a branch. "Unsure" is stated in the item.

1. src-go/internal/teams/picker/picker.go:231-263 (run.loop, once, listPass), the originating
   instance. Found by reading. Sure. Screen: Kit.Choose at 274 (fzf). Work: once calls
   collectWithSudo (361-380, Board.Collect, many subprocesses, twice when sudo is needed) on
   every pass before the next screen. Esc from a row's menu is ui.ErrBack, which goes to
   r.count(flow.Done) (285-289) and round the loop with no wait.
2. src-go/internal/teams/picker/sessions.go:78 and 186 (Kit.Choose, the Running sessions view),
   reached through once, then sessionsPass (picker.go:254-256). Text search. Sure for the loop;
   the file was not read beyond the call sites.
3. src-go/internal/teams/picker/picker.go:128-154 (pickerSession) with offers.go:88 and 292.
   Text search then reading. Sure. Board.Refresh, offerPathLimits, offerDescriptions
   (gum Choose 88, Input 292), offerUpdate, pickerFrame (Checkouts.Find, a git subprocess, 188),
   then the first fzf.
4. src-go/internal/teams/picker/picker.go:296-310 with Kit.BackToList (flow/kit.go:236-255).
   Reading. Sure for the part after the wait: a refusal prints, waits for Enter on /dev/tty
   (cooked, intended), then returns to the loop of item 1.
5. src-go/internal/teams/ops/takedown.go:121, 171, 192, 200 (gum Confirm, Confirm, Choose,
   Input), 323 (picker Confirm), 397 (picker Pick) and ops/playlock.go:186 (Picker.Pick).
   Text search, then read at the call sites only. Sure for takedown: the screens are separated by
   backup and Declare work, and by the apply play (ops.go:266-285 runPlay, streamed ansible).
6. src-go/internal/teams/create/wizard.go (gum chain). Text search then reading. Sure.
   askScope 459 (gum Choose), askOwner 482, askTeam 556-579 (Declare.SuggestTeam, Gum.Input
   through askValid 690-705, Declare.Check 579), askDescription 599, askHumans 609-636
   (id -un, Declare.Check), askAllowFrom 655, askAddress 674, confirmSummary 863-883
   (CreateDryRun, Gum.Box, Gum.Confirm), write and declareAndApply 929-953 (Declare.Create,
   Element, apply play), afterWrite 955, afterCreate and offerJoin 993-1048 (Checkouts.Find,
   Declare.TeamsFor, Gum.Confirm 1027). Every answer is validated by a Python subprocess before
   the next screen; gh api runs between steps (ghLookup 437, needGh 247-251); Gum.Box and
   Gum.Explain (355-358, 398) are extra gum children between questions.
7. src-go/internal/teams/lxcadd/adder.go (gum plus Bubble Tea). Text search then reading. Sure.
   SudoReady 350, Gum.Choose Container 489, Gum.Input User 600 (check 516 LxcCheck),
   pathinput.Asker.Ask Checkout step (wired at 127; Bubble Tea program built at
   ui/pathinput/pathinput.go:556-561; check 529 LxcCheck), LxcDerive 538, Gum.Choose Role 567,
   Gum.Confirm takeover 576, LxcAddDryRun 668, Gum.Box 683, Gum.Confirm apply 711 (its text says
   sudo asks for a password next), SudoReady 742, then the apply play.
8. src-go/internal/teams/settings/settings.go. Text search then reading. Sure for the sequence:
   Kit.Choose 138, Gum.Input 271 with Declare.CheckSet 285, Kit.Choose, Kit.Confirm 332 and 352,
   Declare.Set 363, apply play, BackToList 392, Declare.Settings re-read 435, Kit.Choose 138 or
   543. Read at call sites only.
9. src-go/internal/teams/chooser/chooser.go:211-249 (bare, the ccy-teams choose-teams path that
   ccy --teams uses with no list). Reading. Sure. entries (Declare subprocesses), take
   (Picker.Choose, list.go:37, fzf multi), settle (consent.go:80 Gum.Confirm; widen.go:51
   Gum.Choose, then Declare.AddRepo at widen.go:116), consent.go:174 Bus.SeatChooseAs, seats,
   item 10, launcher exec. Loops up to maxRounds (228) with the machine re-read.
10. src-go/internal/teams/choose/choose.go:241 and 281. Text search. Sure for a list of more than
    one team: Seat is called once per team (chooser.go:182-199 resolve), and each call lists
    seats through agentbus (subprocess, sudo when needed), asks Gum.Choose (seat), then
    offerRole Gum.Choose (role). For one team the gap is tput cols plus any Bus call.
11. src-go/internal/teams/login/login.go:186, 318, 356, 456 (Kit.Confirm and Gum.Confirm) with
    WaitForEnter at 72 and wl-copy at 504. Text search. Likely. Not read in full.
12. src-go/internal/teams/restart/restart.go:178, shutdown/shutdown.go:189, winddown/winddown.go:206,
    seats/seats.go:187, 252, 270, seats/role.go:40. Text search. Likely: a picker confirm, then tmux
    or bus work (restart/jobs.go, shutdown/stop.go), then back to the item 1 loop.
13. src-go/internal/sessions/picker.go:64-108 (a.picker). Reading. Sure. a.rows (table.go:52-77:
    Probe.Detail plus Tmux.List, subprocesses, every pass), choose 155 (fzf), act 236,
    confirmEnd 282-313 (picker Confirm, Tmux.KillSession, registry write, redraw) or
    confirmTakeOver 341-368 (Tmux.OtherTerminals, picker Confirm, takeOver), then rows again.
    Same shape as item 1.
14. src-go/internal/freeze/loop.go:12-60 (Session.Loop and pass), shared by podfreeze and
    lxcfreeze (freeze/app/app.go:60-130; src-go/internal/podfreeze/app.go; src-go/internal/lxcfreeze/app.go).
    Text search (Fzf.Choose at freeze/chooser.go:89 and 201) then reading. Sure. Refresh (loop.go:35),
    Chooser.Group (chooser.go:87-103, fzf), Engine.Select (loop.go:72), Chooser.Members
    (chooser.go:199-215, fzf multi), DoAction (freeze/action.go; lxcfreeze goes through sudo),
    Refresh, Group again. The numbered fallback (chooser.go:166-193, 241-270) reads /dev/tty
    cooked, which is correct there.
15. src-go/internal/open/app.go:150-181 (openTarget per target) with open/choose.go:57
    (Picker.Pick). Reading. Likely, only with several targets and no confident handler: resolve
    (mimetype, gio mime), fzf, launch (open/launch.go:96, handler on the terminal, may be raw
    such as vim or less), next target's resolve, fzf.
16. src-go/internal/ftpcamera/args.go:176-196 (launch: mode, viewer, hotspot; chooser 203-225).
    Reading. Likely, short: up to three gum or fzf screens in a row with only tput cols between.
17. Screen then cooked prompt, no second raw screen. Unsure whether it is in the class.
    sessions/picker.go:213 and 267-276 (noSessions, startHere: picker Confirm then Exec of ccy,
    which asks read -rp questions); teams/seats/seats.go:59-74 (StartCcy); teams/create/wizard.go:111.
    Keys typed after the last screen would be echoed into the next prompt.
18. files/var/local/claude-yolo/lib/tmux-session.bash:213-258 (ccy_tmux_offer), 295-315
    (ccy_tmux_pick, the only fzf in the launcher), 380-398. Text search for fzf, then reading.
    Unsure: one fzf screen only. Work before it: ccy_tmux_project_sessions and a
    ccy_tmux_session_seats per session (219-221). After it, a refused attach falls through to
    the launcher exec (397), which asks read -rp questions. Same shape as item 17.

## B. Single-screen programs, no hazard of this class

- ccproject/app.go:166-169 (one [Y/n] line from stdin); ccylaunch/buildlock.go:97-145 (ui.Lines,
  cooked by design); open/choose.go numbered fallback; ghaccount (no gum or fzf, foreground gh
  only, see C).
- vmtest, convert, wsi*, stt, watchlist, watermark, rclone*, git-fetch-age, git-prompt-fetch,
  pingbus-status-line: no gum, fzf or Bubble Tea call sites.
- files/home/bashrc-includes/history-search.bash:67-76: one fzf from readline.
- files/var/local/claude-yolo/lib/agent-bus-seats.bash and agent-choice.bash mention gum in comments
  only; the chooser is Go (ccy-teams choose-teams). No bash file calls gum.
- Python wsi-model-manager and wsi-server-manager (files/home/.local/bin): input() loops, cooked,
  no raw screens. ui.Gum.Spin (ui/gumx.go:24) has no production caller.

## C. Places where a child or this process is given the terminal for cooked input

A fix that holds echo off between screens must give echo back for each of these.

Go (paths under src-go/internal):
- ui/lines.go:177-225 (Lines.readLine); user ccylaunch/buildlock.go:97, 145.
- teams/flow/kit.go:241-286 (WaitForEnter, byte-at-a-time read on OpenTTY = /dev/tty,
  teams/host/host.go:236-244). Callers: BackToList kit.go:236, login.go:72, settings.go:392.
- freeze/chooser.go:105-124 (guard opens /dev/tty), 166-193, 241-301 (numbered menus, readLine);
  freeze/host.go:21,48.
- open/choose.go:80-90 (needTerminal opens /dev/tty), 141-170 (readLine).
- sudo/sudo.go:56-62 (openDevTTY), 68-93 (Ready: sudo -v with Stdin the terminal; sudo asks for a
  password and turns echo off itself), 165-176 (noTerminal). Callers: kit.go:219 (SudoReady),
  picker.go:359-380 (collectWithSudo), lxcadd/adder.go:350, 742, winddown.go:326, ftpcamera/app.go:99-101,158.
- teams/ops/ops.go:266-285 (runPlay) and 354-384 (runElementPlay): ansible plays with Stdin set, may
  ask for the become password.
- ghaccount/app.go:664 (gh auth login, "Press Enter when prompted") and 720 (gh auth refresh) through
  execx.Foreground (execx/foreground.go:44-81, the child inherits stdin, stdout, stderr).
- tmuxccy/tmuxccy.go:230-234 (attach-session with Stdin, Stdout, Stderr; tmux takes the tty raw
  itself) and tmuxccy/takeover.go:84. Callers: sessions/picker.go:319, teams/seats/seats.go:97.
- Process replacement (the new program owns the terminal and its modes): sessions/picker.go:271,
  teams/host/host.go:246-263 (execLauncher; seats.go:69, wizard.go:111), ccylaunch/front.go:477
  (Go front execs the bash launcher), ftpcamera/app.go:308, 383.
- vmtest/member.go:263 (ssh with Stdin) and vmtest/lab.go:216.
- gum and fzf children read the tty themselves (ui/ui.go:122-126, ui/picker.go:129, ui/choose.go:35);
  Bubble Tea input comes from tea.WithInput (ui/pathinput/pathinput.go:556).

Bash (cooked read prompts and tmux):
- files/var/local/claude-yolo/claude-yolo: 490, 1036, 1074, 1278, 1553, 1590, 1615, 1718, 1735, 2490,
  2625, 2798, 2817, 3033, 3077, 3116, 3149, 3958.
- files/var/local/claude-yolo/lib: common.bash:224; dockerfile-custom.bash:41, 121, 161, 722, 767;
  docker-health.bash:210, 233, 440, 585, 611; network-management.bash:270, 395, 398, 659, 779, 1038;
  token-management.bash:1242, 1916; ssh-handling.bash:492; tmux-session.bash:124 (attach-session) and
  427 (exec tmux attach-session).
- files/var/local/claude-code/cc:46 (token chooser, read -p).
- files/home/.local/bin: scp-with-key:76, ssh-copy-id-with-key:79, ssh-with-key:75, ssh-with-agent.bash:128,
  ssh-agent-start:91, ssh-keys-rekey:79 (read -rsp, deliberately no echo), 257, reclaim:105, 120, 507,
  nord:533, 546, 571, raw-prune:133; fedora-desktop-health.j2:70; files/usr/local/bin/shutdown-with-update:416.

## D. What each technique found

Found by text search only (reading would not have swept the tree):
- The full list of call sites of Pick, Choose, Confirm, Input, Foreground, /dev/tty and os.Stdin:
  about 70 non-test lines. Items 5, 8, 10, 11, 12, 16 started there; I read call sites and
  surrounding functions for 5, 8, 10, 16 and not whole files for 11, 12.
- The absence of any termios, stty, MakeRaw or ECHO handling in first-party Go (zero matches outside vendor/).
- The bash read -rp and /dev/tty prompts in section C; that fzf appears in bash only in
  tmux-session.bash and history-search.bash; that no bash calls gum.
- The per-screen tput cols gap (grep for headingWidth).

Found by reading only:
- That the hazard in items 1, 13 and 14 is the shape of the loop (Collect, rows, Refresh at the
  top of each pass), not any call that names a terminal. A search for fzf or Pick shows one screen;
  only reading once, picker and pass shows the work between passes.
- That ErrBack (Esc from a row's menu) goes straight back to the collecting loop (picker.go:285-289),
  the originating report's path.
- That a row's menu is a second Pick straight after the first (ui/picker.go:364).
- That the wizard and lxc-add validate each answer by subprocess before the next question (items 6, 7),
  and that pathinput's Bubble Tea program is one of the screens.
- That Gum.Box and Gum.Explain between questions are extra gum children, each a gap.
- That the hazard also sits at a screen-then-cooked-prompt boundary (items 17, 18).

## E. Searches run

- grep for fzf, gum, whiptail, bubbletea, tea.NewProgram, term., termios, stty, /dev/tty, WaitForEnter,
  MakeRaw over src-go Go files (too broad: term. matched the package; narrowed next).
- grep for /dev/tty, MakeRaw, termios, stty, ECHO, term.Is/Get/Make/Restore over cmd and internal.
- grep for .Pick(, .Choose(, .Confirm(, .Input(, .Lines(, WaitForEnter, tea.NewProgram, .Foreground(,
  syscall.Exec, .Interactive, os.Stdin under src-go/internal.
- grep for ansible, Interactive, Exec(, attach over teams, ccylaunch, sudo, ccproject, engine,
  ghaccount, open, vmtest, ftpcamera, freeze.
- grep for ReadString, bufio, Scan, .Ask(, YesNo, .Choose(, Confirm, OpenTTY over ccproject, engine, open,
  ghaccount, vmtest, ftpcamera, ccylaunch, freeze.
- grep for ui.Lines{ and &Lines{ (one production user, ccylaunch/buildlock.go:97).
- grep -l for fzf, gum, whiptail, dialog over files, scripts, helpers; then gum, ccy_tmux_pick, fzf over
  files/var/local, files/home/.local, files/usr, scripts.
- grep for gum choose/input/confirm/filter/spin/write, fzf, read -r/-s/-p over files and scripts.
- grep -l for curses, textual, termios, tty, prompt_toolkit, questionary, inquirer, input( over files,
  helpers, scripts, extensions.
- grep for gum spin.
- Read in full or the relevant part: ui/ui.go, ui/choose.go, ui/picker.go (1-230), ui/lines.go (140-232),
  ui/gumx.go, ui/pathinput/pathinput.go (540-574), execx/foreground.go, teams/picker/picker.go (120-380),
  teams/flow/kit.go (120-287), teams/host/host.go (70-264), sessions/picker.go (60-170, 236-385),
  tmuxccy/tmuxccy.go (205-252), freeze/chooser.go (60-302), freeze/loop.go, freeze/app/app.go (60-130),
  sudo/sudo.go (50-177), teams/ops/ops.go (255-385), teams/seats/seats.go (40-150), teams/chooser/chooser.go
  (175-255), open/app.go (140-199), open/choose.go (40-120), ghaccount/app.go (640-730), ftpcamera/args.go
  (165-225), ccylaunch/buildlock.go (85-160), files/var/local/claude-yolo/lib/tmux-session.bash (205-427).
- Call sites only, not read in full: teams/login, restart, shutdown, winddown, settings, takedown, lxcadd
  beyond the cited lines, picker/offers.go and sessions.go, the bash launcher's prompts.

Stop condition: the last three searches (other languages, gum spin, the second fzf and gum bash pass)
found nothing new.
