## Security review: Plan 00266 Phase A (v2-smoke at HEAD 89567a33a)

**Verdict: FIX-BEFORE-SHIP.** The four Go pieces are sound as far as they go: the approval store, the strict record reads, last-launch's read-once pin and the fixed launcher table. But the threat model is still broken in three places:

- One container write can bind any host file into the next container, with nobody asked.
- One recorded word can run a host command through tmux. This contradicts DESIGN.md:15 and :58 ("No free-form string reaches a shell").
- An attended launch applies, and then re-pins, compose content a container rewrote.

Both probes I ran are below. `go test ./internal/checkoutpin/... ./internal/registry/...` passes, but no test covers any of these paths.

### CRITICAL

**1. A link at `.claude/ccy/ccy.env.local` binds any host file into the next container, restores included.** `files/var/local/claude-yolo/lib/common.bash:654-663`, called at `claude-yolo:2486-2487`.
- **The code:** `[ -e ]` and `[ ! -f ]` both follow links, then the code emits `-v "$PWD/.claude/ccy/ccy.env.local:/workspace/...:ro"`. The mounts pin never sees this path.
- **Attack:**
  1. In the container, `ln -s /home/<user>/.ssh/id_ed25519 /workspace/.claude/ccy/ccy.env.local` (or point it at a token file, a vault password file, or gh's `hosts.yml`).
  2. The next launch of the checkout (attended, restore or restart) binds the link's target.
  3. The agent reads `/workspace/.claude/ccy/ccy.env.local`. The checkout-owned `ccy.env` is sourced first (`entrypoint.sh:450-454`), so it can `set +e` and copy the file before the key fails to source.
  4. On an SELinux host the `,z` suffix also relabels the target shared.
- **Impact:** any file the host user can read is exposed, with no approval and no warning.
- **Fix:** move this into checkoutpin. Open the file through `os.Root`, refuse a link or any non-regular file, copy the bytes into a private host temp file (as `CONFIG_TEMP` does), and bind the copy. Binding the checkout path keeps a check-to-use race open with any live container of the same checkout.

### HIGH

**2. A replayed word ending in `;` ends tmux's `new-session`, and the words after it run as tmux commands on the host.**
- **Where:** `src-go/internal/tmuxccy/start.go:77-84`; `src-go/internal/registry/replaycheck.go:69-71` (`--` returns nil, so every later word passes); `lib/tmux-session.bash:477` (same shape).
- **Proven:** I ran `tmux -L <probe> new-session -d -- bash -c 'sleep 30' ccy-tmux /bin/true -- 'word;' set-option -g @injected yes ';' …`. `show-options -gv @injected` printed `yes`, and the pane got `word` with the `;` stripped.
- **Attack:** under A.4/A.5's own premise ("any process of the user can write a session record"):
  1. Write a record with `arg=--`, `arg=x;`, `arg=run-shell`, `arg=<command>`.
  2. `ReplayRefusal` accepts it.
  3. `ccy-sessions restore` or a team restart runs the command on the host, whatever the fixed launcher table says.
- **Impact:** host code execution from a record. This voids A.4/A.5's guarantee. A person's own `ccy -- 'x;'` also breaks its own launch.
- **Fix:** escape each argv word ending in `;` as `…\;` (tmux turns that back into a literal `;`) in one helper that both `StartDetached` and the bash call use. Add a test against real tmux. Add a detector for any tmux argv built from words that are not constants.

**3. An attended launch applies container-written compose files unshown, defaults to Yes, and records them as approved.**
- **Where:** `lib/network-management.bash:912-913` (`unattended=false` whenever a terminal exists); `checkoutpin/command.go:161-165` (`compose false` records with no comparison and no warning); the `[Y/n]` prompt at `network-management.bash:1036`, where Enter means Y.
- **Attack:**
  1. The container adds `volumes: ["/home/<user>:/h"]`, or under `--engine docker`, `- /:/host`.
  2. The person launches and presses Enter.
  3. `up -d` runs it as the host user (with docker, as root), and the new digest is recorded, so every later `--compose start` restore runs it too.
- **Impact:** the pin only protects unattended launches, and any attended Enter launders the content into the store.
- **Fix:** when an attended launch finds content that differs from the recorded digest, say so, show the files (or a diff), and default to No. Never record unless the content matched or the person saw it and answered yes.

**4. The mounts digest covers the file's text, not the paths its sources resolve to.** `checkoutpin/command.go:110-123`, `pin.go:208-211`.
- **The code:** sources are resolved and judged again at launch. But the judge is only a denylist, and the resolved targets are not part of the approval.
- **Attack:**
  1. A person approves a source that has a container-writable component: a path under the checkout (`HostProblems` refuses only the project and directories above it), or a path inside another rw source (`~/shared:/shared` plus `~/shared/models:/m`).
  2. The container replaces that component with a link to any path not on the denylist. `/tmp/tmux-<uid>` works, because `/tmp` is not denied (`mounts.go:35`).
  3. The digest is unchanged, so the next restore binds the new target. Through the host tmux socket that is host code execution.
  4. Separately, the resolved path printed at check time can be swapped before `podman run`.
- **Fix:**
  - Put each resolved source in the approved digest, and refuse the launch when one resolves elsewhere.
  - Refuse a source that lies under the checkout or under another rw source.
  - Add `/tmp`, `/var/tmp` and `/dev/shm` to `hostDenied`.
  - Other paths from which the host later runs code are also allowed: `~/.config/dconf`, `~/.local/share/nautilus`, editor configs, `~/.config/rclone`, `~/.npmrc`, `~/.cargo`. The person sees these at approval, which matters less once the link redirect above is closed.

**5. The compose digest leaves out most of what `up -d` reads.** `pin.go:323-370` follows only `include` and `extends.file`. Not covered:
- `build:` contexts and their Dockerfiles. A container edits the Dockerfile and the approved stack runs its code.
- `env_file:`, `configs`/`secrets` `file:`.
- Relative bind sources (`./data:/d`), which a container can turn into links to any host path. The digest is unchanged.

Fix: either refuse these unattended, or bring each one into the digest (resolved realpaths, Dockerfile bytes) and refuse links in bind sources. I could not check podman-compose's default file list here. If it includes `container-compose.*`, as I believe, those files are unpinned.

**6. The host writes `.claude/ccy/.gitignore` and `ccy.env.local.dist` through container-placed links, on every launch.** `lib/common.bash:461-491` and `680-695`.
- **The code:** `echo … > "$ccy_gitignore"`, `>>` appends, and `printf … >"$dist"` all follow a link. So does a `.claude/ccy` directory that is itself a link.
- **Attack:** the container links `.gitignore` to any host file. That file is truncated and rewritten with fixed content, or created if absent (for example in `~/.bashrc.d/`, where its bare `*` line runs as a glob command).
- **Impact:** arbitrary host files are destroyed or created by the launcher, restores included.
- **Fix:** write through Go with `os.Root` and a temp-plus-rename (as `WriteLastLaunch` does), or refuse with `[ -L ]` on the file and on every parent up to `$PWD`.

### MEDIUM

**7. The custom `.claude/ccy/Dockerfile` is built and run unpinned, unattended included.** `claude-yolo:2028-2135`.
- **The code:** a changed hash triggers `podman build -f .claude/ccy/Dockerfile .` with the checkout as context, and the result becomes the session image.
- **Impact:** a container can choose its next image's entrypoint and replace in-image controls such as the D62 bus refusal at `entrypoint.sh:552-564`. It can also run RUN steps against the shared buildah cache mounts.
- **Not verified:** which of the session's controls live in the image rather than on the host.
- **Fix:** pin the Dockerfile as a fourth kind.

**8. The Quick Launch offer is accepted on Enter (and always at the gum summary), even for a last-launch file ccy did not write, and the write re-pins it.**
- **Where:** `claude-yolo:1268-1278`, where `""` means yes; `launch-summary.bash:39-41`, where the summary path sets `use_config=y`; `launchconfig.go:62-71`, which only warns; then the write at `claude-yolo:3369` records the digest.
- **Impact:** a container-chosen `LAST_SSH_KEYS` or `LAST_NETWORK` becomes "approved" for later restores on one keystroke.
- **Fix:** when the content is unrecorded, skip the summary and require an explicit `y`.

**9. `ccy-teams approve compose` lists file names only.** `command.go:219-223` tells the person to "read them". The bytes recorded are not the bytes shown, and the container can change them between the person's own `cat` and the approve. Fix: print what is being approved, or a diff against the last approved copy.

**10. The detector class is too narrow, so findings 1, 6 and 7 passed it.**
- **The rules:** `qa/rules/checkoutpin.go:20-25` and `.semgrep/bash-conventions.yml:605-609` define the class by file name: mounts, last-launch, compose.
- **What slips past:**
  - the `ccy.env.local` bind, the `.gitignore`/`.dist` writes and the Dockerfile build;
  - `container-compose.*`;
  - `podman compose up`, since the regex has no `podman[ \t]+compose`;
  - any path not spelled exactly, such as `./compose.yaml`.
- **Fix:** define the class as "host acts on, binds or writes a container-writable checkout path", and sweep `.claude/ccy/*` and `$PWD/` uses by that definition.

### LOW

**11. Format characters pass the display checks.** `lastlaunch.go:85` uses `unicode.IsControl`, which covers only Cc, and `mounts.go:128` checks only below 0x20 and 0x7f. Bidi overrides (Cf) can make an approval screen show a different value. Fix: refuse anything that is not `unicode.IsPrint`.

**12. Parser divergence on `--`.** `ccyReplayRefusal` lets a value flag consume a following `--` (`replaycheck.go:83-87`), while the bash parser treats `--` as the separator first (`claude-yolo:671`). Today the effect is harmless (the words go to the container's claude), but the two parsers should agree.

### On your specific questions

- **Do words after `--` reach the host?** Not the launcher's own option parsing: `claude-yolo:668-670` collects them raw into `CLAUDE_ARGS`. They do reach tmux's command parser (finding 2).
- **Can unattended mode be spoofed?** Not from the checkout. It is decided by `CCY_SESSION_RESTORE`, the restart variable, a non-terminal stdin, or `--headless`, all from host environment or argv (`claude-yolo:1204-1219`, `common-pure.bash:114-135`).
- **Does attended mode show the real content?** It shows the real values once read, with a warning on stderr. The values come from the one verified read, with no re-read. But Enter accepts them (finding 8).
- **Can a container or a non-terminal approve?** No.
  - Non-terminal: `app.go:495` requires stdin and stderr to be terminals, and with no `Confirm` the approve exits 1 (`command.go:177-178`).
  - Container: the store sits under the host state directory, which no mounts line may bind (`mounts.go:29`, `command.go:107`).
  - Any host process of the same user can still write the store directly, which is outside this threat model.

### What I checked and found clean

- **Approval store:** 0700 directories, 0600 files, written atomically. Reads are exact, so a wrong checkout, header or digest is an error, never a pass (`pin.go:170-206`).
- **Checkout path:** records are keyed by the real path (`RealCheckout`). A relative or `..` checkout is resolved first, and the front's flags require an absolute path.
- **Last-launch:**
  - The file is read once, and the bytes judged are the bytes handed back (`launchconfig.go:96-121`).
  - Writes go through `os.Root` with a temp-plus-rename, so a planted link is replaced, not followed (`lastlaunch.go:113-135`).
  - Keys are whitelisted, and bash assigns them with `printf -v`, never sourcing.
- **Mounts:** the file is read through `os.Root`, a link in its place is refused, and the lines are parsed from the same bytes the digest covers. There is no gap between check and use for the file itself.
- **Launcher table (A.4):** `registry.Launcher` uses only the fixed table, and both restore and restart call it (`restore.go`, `jobs.go:252`).

### What I could not verify

- **Podman following links in a bind source:** findings 1 and 4 rest on this. It follows from mount(2) semantics, but I could not run podman here.
- **podman-compose's default file list** (`container-compose.*`).
- **Image versus host controls:** which session controls a custom image could remove (finding 7).
- **Container-controlled record arguments:** whether anything a container controls can reach a record's `arg=` lines. I found no such path, so finding 2 is a host-writer bypass of A.4/A.5 rather than a container-to-host path.

### Mechanical gates

- `go test ./internal/checkoutpin/... ./internal/registry/...`: both packages ok.
- `qa-all.bash` and `plan-qa --sweep`: not run. This review was asked to probe security, and a full-suite run is refused inside a sub-agent (R-SUBAGENT-FULL-QA).

Relevant files:
- /workspace/files/var/local/claude-yolo/lib/common.bash
- /workspace/files/var/local/claude-yolo/lib/network-management.bash
- /workspace/files/var/local/claude-yolo/claude-yolo
- /workspace/src-go/internal/tmuxccy/start.go
- /workspace/src-go/internal/registry/replaycheck.go
- /workspace/src-go/internal/checkoutpin/command.go
- /workspace/src-go/internal/checkoutpin/pin.go
- /workspace/src-go/internal/checkoutpin/mounts.go
- /workspace/src-go/qa/rules/checkoutpin.go
- /workspace/.semgrep/bash-conventions.yml
- /workspace/CLAUDE/Plan/00266-seat-restart-after-reboot/DESIGN.md