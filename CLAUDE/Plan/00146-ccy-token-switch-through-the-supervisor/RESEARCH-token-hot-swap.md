# Research: hot-swapping the Claude token of a running ccy session

Scope: read-only investigation. Nothing tracked was edited, nothing was run against the host.
No token values or secret files were read. Line numbers refer to the tree at `F44` HEAD
(`e249d269`), CCY_VERSION 3.70.1, supervisor 3.67.0, installed Claude Code 2.1.287.

> **Owner correction (supersedes the verdict in section 5).** This research rejected an
> in-container restart (option b2) too quickly. The supervisor starts `claude` itself
> (`pty.fork` + `os.execvp`, about `claude-supervise.py:6616-6622`), so it decides the
> environment of every `claude` it spawns, even though the container's own environment is
> immutable. The plan therefore prefers a supervisor-driven respawn (`/exit`, then
> `claude --resume <session-id>` with the new token, the container kept alive) and keeps
> b1, the container relaunch in section 5, as the fallback if upstream declines. The b2
> problems listed in section 3 are addressed in the plan: the token is delivered as a
> one-shot 0600 file the supervisor reads and deletes; `ccy-claude` stops reading
> `/proc/1/environ`; the stale `ccy-token` label is handled as its own task.

## Verdict

**The token cannot be changed inside the running `claude` process** (it is fixed once the
process starts, unless you go through `/login`). **It can be swapped without losing the
conversation** by restarting `claude` with the new token and `--resume <session-id>`. The
transcript is on disk under `/workspace/.claude/ccy/projects/`, so the conversation survives.
What does not survive is everything that lives only in the `claude` process: the turn in
flight, background Bash tasks, MCP servers and running sub-agents. Prompt caching also starts
cold on the new account.

The one thing that works today with no code change is `/login` typed in the session. It does
switch credentials in-process, but it carries significant drawbacks (see 3c), so treat it as an
escape hatch and not as the design.

---

## 1. How ccy selects and injects the token today

### Storage (host)

- Token pool: `~/.claude-tokens/ccy/tokens/NAME.YYYY-MM-DD.token`, with the expiry date in
  the filename. Each file holds a single `sk-ant-oat01-…` long-lived OAuth token minted by
  `claude setup-token`. The pool is set in `files/var/local/claude-yolo/claude-yolo:182-183`.
  The directory is created mode 700 at `:839-840`. It is documented at `docs/ccy.md:518-531`.
- The pool is separate from desktop Claude Code (`claude-yolo:4`, `entrypoint.sh:4`).

### Selection (host, at launch)

- `--token NAME` (`claude-yolo:628-629`, `:572-574`). It resolves to the first matching
  `NAME.*.token` (`:1138-1163`). Expiry is checked by `is_token_valid` (`:1165-1185`).
- With no `--token`: the quick-launch prompt can reuse `LAST_TOKEN` from
  `.claude/ccy/.last-launch.conf` (`:957-1006`; the writer `save_launch_config` is at
  `:464-485` and is called at `:2960-2971`). Otherwise the `select_token` menu runs
  (`lib/token-management.bash:1062`, called at `claude-yolo:1223`). That menu can show
  per-account 5-hour and weekly utilisation on demand (`u`), read from the
  `anthropic-ratelimit-unified-*` response headers (`token-management.bash:400-527`). This is
  directly useful for choosing an account that is not at its 429 limit.
- The token is loaded with `cat` (`claude-yolo:1252`). It then gets a format check
  (`sk-ant-oat01-`, `:1258`), a length check (90-120 bytes, `:1264-1271`) and a live check
  (`validate_token`, which runs `claude --version` in a throwaway container,
  `token-management.bash:670-693`).
- A recovery menu offers recreate, select another, or abort (`claude-yolo:1284-1382`).

### Injection (host into container)

- **Environment variable only. No credential file is mounted.** The design is stated at
  `claude-yolo:1959-1967` and `:2076-2085`, and in `docs/ccy.md:340`.
- `export CLAUDE_CODE_OAUTH_TOKEN="$CLAUDE_OAUTH_TOKEN"` (`:3123`), then
  `podman run … -e CLAUDE_CODE_OAUTH_TOKEN` passed by name, so the value is not in argv
  (BSH-09, `:3118-3124`, `:3245`).
- The token name is also stamped as a run-time label, `--label ccy-token=NAME` (`:3186`,
  `:3237`). Host tooling such as podfreeze filters on it.
- In the container, `/root/.claude` is a symlink to `/workspace/.claude/ccy`
  (`entrypoint.sh:281-294`). `/root/.claude/.credentials.json` does **not** exist (checked
  with `ls`).
- Process tree observed in this container: tini (PID 1), then `entrypoint.sh`, which
  `exec`s into `claude-supervise.py` (`entrypoint.sh:502-541`), which spawns `claude` once on
  a PTY. `CLAUDE_CODE_OAUTH_TOKEN` is present in the environments of PID 1, the supervisor
  and `claude`. Claude Code strips it from its Bash-tool children; I checked variable names
  only.
- Child-claude mode (`ccy-claude`) gets its credential by **reading `/proc/1/environ`**
  (`files/opt/claude-yolo/optional/child-claude/bin/ccy-claude:9-37`, `:81-103`). So the
  token in PID 1's environment is what every child claude uses.

## 2. How Claude Code reads the credential (evidence: installed 2.1.287 binary)

The binary is a single native executable, `.../@anthropic-ai/claude-code/bin/claude.exe`,
with minified JS inside it. Offsets below are byte offsets in that file. The minified
function names change on every release, so these are evidence for this version only.

- **The token is read once and then memoised.** `IH()` (offset ~201654417) builds the
  credential. It checks `CLAUDE_CODE_OAUTH_TOKEN` first, then the
  `CLAUDE_CODE_OAUTH_TOKEN_FILE_DESCRIPTOR` source, then `.credentials.json`. `fn()` caches
  the result in a per-host memo (`e.value`, ~201655640). Nothing re-reads the environment per
  request.
- The memo is cleared only by auth events: a 401 recovery, `/login` saving credentials, and
  similar.
- **Credentials-file change detection is switched off when the env var is set.**
  `JJe()` returns false if `CLAUDE_CODE_OAUTH_TOKEN` or the file-descriptor variable is set
  (~201656600). As a result, the mtime-based re-read of `.credentials.json` never runs in
  ccy's mode.
- **The 401 path explicitly refuses to adopt a different credential in ccy's mode.**
  - `eKo()` (~201659817) is true when `CLAUDE_CODE_OAUTH_TOKEN` is set, the session is not a
    remote session and there is no unix socket.
  - In that case the 401 handler `YH()` logs *"OAuth 401: keeping the user-supplied
    CLAUDE_CODE_OAUTH_TOKEN instead of adopting the stored credential. Mint a fresh token
    with `claude setup-token` and restart with it, or unset the variable and run /login."*
  - The "rotated env token" wait (`wts`, `CLAUDE_CODE_OAUTH_401_WAIT_MS`) defaults to 0
    outside remote sessions. Even when enabled, it polls the process's own `process.env`,
    which nothing outside the process can change.
- **A 429 is not a 401.** None of the recovery machinery runs on a rate limit. A
  rate-limited session simply keeps using the same token.
- **`/login` does swap in-process.** `LHe()` (~offset of the "Couldn't save your login"
  string) runs when `/login` saves. If `CLAUDE_CODE_OAUTH_TOKEN` was set and the save
  succeeded, it **deletes** `process.env.CLAUDE_CODE_OAUTH_TOKEN` and resets the memo. The
  stored claude.ai login then becomes the credential.
- `apiKeyHelper` (with `CLAUDE_CODE_API_KEY_HELPER_TTL_MS`) is re-run on a TTL and on 401.
  It is API-key auth, though: it is reported as "from apiKeyHelper" and the help text lists
  it alongside `ANTHROPIC_API_KEY`. It is not the subscription OAuth path that ccy's
  `sk-ant-oat01` tokens use. Unverified and not recommended.
- Resume flags (`claude --help`): `-c/--continue` resumes the most recent conversation **in
  the current directory**. `-r/--resume <id>` resumes a specific session id. `--fork-session`
  would mint a new id, so do not use it here.
- Claude Code writes `~/.claude/sessions/<pid>.json` with a `sessionId` key (key names
  checked in `/workspace/.claude/ccy/sessions/*.json`). Given the `claude` pid, the session
  id can be recovered without guessing.

## 3. Hot-swap options

### (a) Rewrite a mounted credentials file in place from the host

**Not viable.**

- Nothing is mounted. Adding a mount would contradict the "no credential files" model
  (`docs/ccy.md:340`).
- Even with a mount, Claude Code ignores the file while `CLAUDE_CODE_OAUTH_TOKEN` is set
  (env first in `IH`, change detection off in `JJe`, disk adoption refused in `YH`).
- Dropping the env var and using the file-descriptor variable or the file instead would give
  no in-process re-read on 429. A swap would still need a restart.

### (b) Restart `claude` with the new token and `--resume <session-id>`

**Viable. This is the recommended family.** Two placements:

- **(b1) Relaunch the container from the host launcher (recommended).**
  - `claude` exits. `claude-yolo` sees a pending "relaunch with token X, resume Y" request
    and starts a fresh `podman run` with `--token X` and `--resume Y`.
  - The token stays in the environment only. The `ccy-token` label, PID 1's environment
    (and so `ccy-claude`), `.last-launch.conf` and the token validation path all come out
    right for free, because it is a normal launch.
  - Precedent: the reboot-restore path already relaunches with `--continue`
    (`lib/session-registry.bash:446-488`).
  - Cost: a few seconds of launcher preflight (image check, SSH, gh). Any process that lived
    only in the container is lost. That is mainly an in-container ssh-agent, which means a
    passphrase re-prompt unless the agent is forwarded (`SSH_AGENT_FORWARDED`). Almost
    everything else already dies with `claude` anyway.
- **(b2) Restart `claude` inside the same container.**
  - The `claude-supervise.py` supervisor spawns `claude` once (`pty.fork` at `:6616-6622`,
    a single `waitpid` at `:6803`, and `main` returns the child's exit code at
    `:6929-6996`). **It has no restart-with-resume path.** Its restart logic covers only its
    policy *worker* subprocess.
  - It is also **daemon-owned**: the header at `:1-6` says "do not edit", and the hooks
    daemon replaces it on upgrade. A claude restart loop could not live there without an
    upstream hooks-daemon change.
  - It could live in `entrypoint.sh`, which is owned by this repo: replace the final
    `exec` (`:537-541`) with a loop.
  - Problems with b2:
    - The new token must reach the container after start, via `podman exec -i` on stdin
      into a tmpfs file.
    - **PID 1's environment keeps the old token for ever**, so `ccy-claude` children stay
      on the rate-limited account. They read `/proc/1/environ`.
    - The `ccy-token` label goes stale, because labels are immutable.
    - The old token stays readable in `/proc/1/environ` alongside the new one.
    - It needs a `REQUIRED_CONTAINER_VERSION` and image bump.
  - It only saves the container restart.

### (c) `/login` inside the session

- Works today, in-process, with no restart and no lost state (see section 2).
- Drawbacks:
  1. It does not use the ccy token pool. It is an interactive browser OAuth flow with
     URL/code copy-paste, once per swap.
  2. It writes a full OAuth credential, including a **refresh token**, to
     `/root/.claude/.credentials.json`, which is `/workspace/.claude/ccy/.credentials.json`.
     That is inside the project checkout. It is gitignored by `*` in `.claude/ccy/.gitignore`,
     but it persists across sessions and goes wherever the project tree is backed up.
  3. On the next ccy launch the env token wins and the file lies around unused.
  4. The `ccy-token` label is now wrong.
- Acceptable as a documented emergency escape hatch, with a note to `/logout` (or delete the
  file) afterwards. Not a design.

### (d) Other avenues considered and rejected

- **`CLAUDE_CODE_OAUTH_401_WAIT_MS` / "rotation":** it polls only the process's own
  environment, and does nothing on 429.
- **Claude Code plugin "mod" API env setter** (`Rro`, which sets `process.env[name]`
  in-process): undocumented internals, and the token memo would not be invalidated. Too
  fragile; reject.
- **`apiKeyHelper`:** the API-key auth class, not the subscription OAuth path (section 2).
- **Switching tokens within one account:** this does not help. Rate limits are per account,
  so the target token must belong to a different account. The `u` usage view in
  `select_token` exists to show which account has headroom.

## 4. Is env injection immutable for a running container?

Yes.

- `podman run -e` fixes the container config and PID 1's environment at creation.
  `podman exec` processes inherit that same configured environment. There is no
  `podman update` for environment variables, and labels are immutable too.
- A running process's own `process.env` can only be changed from inside that process.

Consequences for the design:

- Any swap that keeps the container must deliver the new token **out of band** (stdin over
  `podman exec -i` into a tmpfs file) and restart `claude` with an overridden environment.
  It then has to live with a stale PID 1 environment (which breaks `ccy-claude`) and a stale
  `ccy-token` label.
- A fresh `podman run` (b1) has none of these problems. That is the main reason to prefer it.

## 5. Recommended approach: b1, "relaunch with token X, resume this conversation"

### User flow

1. In a running ccy tmux session, the user picks **"Switch Claude account"**. This can come
   from a menu, from `ccy --switch-token` in another terminal, or from
   `ccy-sessions` → session → switch token.
2. The host-side command:
   - identifies the session's container. The tmux session name equals the container name
     (`lib/tmux-session.bash:30-36`). Cross-check the `ccy=true` and `ccy-project` labels.
   - shows `select_token` with usage (`u`), marking the current `ccy-token` label value.
   - validates the chosen token with the existing format, length and `validate_token`
     checks.
3. It resolves the live session id:
   - `podman exec <ctr>` finds the `claude` child of the supervisor and reads
     `/root/.claude/sessions/<pid>.json` → `sessionId`.
   - Alternatively, read it from the project's `.claude/ccy/sessions/` on the host.
   - Fail fast if it is ambiguous. **Do not fall back to `--continue`**: with several
     sessions in one project, `--continue` can pick the wrong conversation.
4. It writes a host-side **relaunch request** in the ccy state dir, for example
   `$XDG_STATE_HOME/ccy/relaunch/<container-name>`, mode 0600. The request holds the token
   NAME and the session id only, **never the token**.
5. It ends `claude` gracefully. Two options:
   - Tell the user to type `/exit` (safest: never kills a turn in flight).
   - Optionally offer `podman exec <ctr> kill -TERM <claude-pid>` when the session is idle.
     The supervisor's idle tracking or the daemon status could gate this. Otherwise ask.
6. Back in the pane, `claude-yolo` regains control after `container_cmd run` returns
   (`claude-yolo:3231-3266`). It finds the request for its own container name, consumes it,
   and re-execs itself with the replay args:
   - the original args with `--token`, `--continue`/`--resume` and the one-shot set removed,
     reusing `ccy_registry_replay_args`;
   - plus `--token X -- --resume <id>`.
   - It skips the "stop compose services?" prompt (`:3268-3321`) for that iteration.
7. The relaunch rewrites `.last-launch.conf` as usual. The session-registry record must also
   be rewritten so a reboot restore uses the new token. `ccy_registry_forget_network`
   (`lib/session-registry.bash:310-356`) is the precedent for an in-place record rewrite;
   add a sibling `ccy_registry_set_token`.

### IaC changes (files)

- `files/var/local/claude-yolo/claude-yolo`:
  - new `--switch-token[=NAME]` mode;
  - the post-run relaunch-request check and re-exec;
  - help text;
  - **CCY_VERSION bump (mandatory)**.
- `files/var/local/claude-yolo/lib/token-management.bash`: put the pieces in functions:
  - a "choose and validate a token by name" function, factored out of the inline logic at
    `claude-yolo:1134-1382` so both paths share it (DRY);
  - a version-header bump.
- `files/var/local/claude-yolo/lib/session-registry.bash`: `ccy_registry_set_token` for the
  record rewrite, plus the request-file helpers. A new lib would also need adding to the
  deploy list in `playbooks/imports/play-claude-yolo.yml:346-367`.
- `files/home/.local/bin/ccy-sessions`: an optional "switch account" action for a session.
- **Menu hook point:** `files/etc/tmux.conf:23-29` (the F12 `display-menu`, deployed by
  `playbooks/imports/play-tmux-sessions.yml`), or a binding that `lib/tmux-session.bash`
  installs on CCY's own `-L ccy` server. The item would run something like
  `display-popup -E "ccy --switch-token --session #{session_name}"`. The other agent is
  researching F12 extension, so this is only the hook point.
- Tests: extend `scripts/test-ccy-session-registry.bash`, plus a pure-function test for the
  replay-args rewrite (`--token` and `--resume` substitution, `--continue` removed).
- Docs:
  - `docs/ccy.md` Tokens section: a "switching account mid-session" subsection, plus a
    `/login` escape-hatch note with its caveats;
  - `docs/ccy-changelog.md`;
  - `files/opt/claude-yolo/docs/CCY-GUIDE.txt` if it covers tokens.
- No `entrypoint.sh` or Dockerfile change, so no `REQUIRED_CONTAINER_VERSION` bump. No
  supervisor change, so nothing upstream.

### Main risks and trade-offs

- **Turn in flight:** killing `claude` mid-turn loses that turn. Prefer `/exit` or an
  idle-gated SIGTERM. The transcript is written incrementally, so `--resume` restores
  everything up to the last completed message.
- **Container-only processes are lost:** the in-container ssh-agent (passphrase re-prompt
  unless forwarded), background Bash tasks, MCP servers, sub-agents. Most of these die with
  `claude` under any option.
- **Cold cache:** the first turn on the new account re-reads the whole context. At ccy's
  600k auto-compact window that is a large one-off spend against the new account's
  allowance.
- **Session-id resolution must be exact:** fail fast and never guess with `--continue` when
  a project has more than one session.
- **Upstream drift:** the conclusions in section 2 come from minified internals of one
  Claude Code release. If upstream ever adds live credential re-read or rotation on 429,
  revisit. The b1 design does not depend on internals, only on the documented
  `--resume <id>` and `CLAUDE_CODE_OAUTH_TOKEN`.
