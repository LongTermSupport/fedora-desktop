# Reviewer fixes for the ccy-teams / ccy-sessions cut-overs

1. Builds: `-buildvcs=false` (with `-trimpath`) on both play builds and every suite build.
   Proof: built ccy-teams and ccy-sessions in a scratch clone, added a commit, rebuilt: the
   sha256 of both binaries was identical. Control without `-buildvcs=false`: the two ccy-teams
   builds across a commit differed.
2. Dead bash deleted from `lib/tmux-session.bash` and `lib/session-registry.bash`: the six named
   functions plus what only they called (container join, network, identity, pid and CPU
   helpers, the full-row layout, `ccy_screen_plain`, `_ccy_input_line_empty`,
   `ccy_transcript_newest`). Go tests already held the same expectations, so
   `scripts/test-ccy-session-network.bash` is deleted (its `ccy_tmux_row` cases moved into
   `test-ccy-teams.bash`). CCY 3.95.8. The transcript context/input/compaction readings and
   `ccy_transcript_dir` stay, commented as the reference the Go test compares against.
   Still dead and left alone (not on the reviewer's list): `ccy_tmux_take_over`,
   `ccy_tmux_idle_words`, `ccy_tmux_client_words`; the take-over suite still tests them.
3. `play-golang.yml` installs only `golang`. golangci-lint moved into `play-python.yml`;
   gcc was already in its dnf list. `meta-deploy.bash` now runs `play-python.yml`.
   `playbook-main.yml` still reaches `play-golang.yml` twice (via play-python and
   play-claude-yolo); both are idempotent dnf runs.
4. `ccy-teams` exits 1 with a message naming `-X main.RootDir` and `play-claude-yolo.yml`
   when built without it.
5. `qa-all.bash` comment fixed; `test-qa-changed.bash` asserts every `src-go/cmd/*` has a suite entry.
