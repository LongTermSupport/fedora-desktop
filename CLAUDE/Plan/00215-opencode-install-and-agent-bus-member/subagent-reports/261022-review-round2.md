VERDICT: FIX-FIRST

The play, the pins, the docs and the queue entry are ready to merge, and both round-1 FIX-FIRSTs are fixed. One upstream fact in the design is wrong, and the delivery rule built on it would break v2's delivered-once guarantee.

**Checks I ran myself**

- **sha256:** the release API digest at v1.19.3 is `sha256:13a8fb29…6c9b`, which matches `opencodeSha256`.
- **Gates:** `ansible-playbook --syntax-check` passes on ansible-core 2.19.14. `qa-version-pins.bash` prints `VERSION-PINS-OK 14 … 14 of 14`. `check-pinned-versions.bash` shows `opencodeVersion 1.19.3 v1.19.3 up-to-date`, so the PLAN.md:116 tick is right.
- **File:** mode 100755, with the standard shebang.
- **Play under 2.19:**
  - `become` is set per task; the play itself runs with `become: false`.
  - `get_url` has a checksum, `unarchive` has `creates`, and the symlink is `force`.
  - The cleanup `find` uses `use_regex` with an anchored versioned pattern. The exclude `^opencode-{{ v | regex_escape }}$` matches the basename, so `opencode-1.19.30` is still removed. The removal loop's label is fine.
  - The managed config renders through `to_nice_json`, the version assert is right, `scope: general`, and nothing is written outside the user's tree except `/etc/opencode`.
- **Upstream facts at the tag:** `installation/index.ts:277-278` uses the `.local/bin` → `curl` method, `cli/network.ts` gives the `127.0.0.1` default and explicit flags beating config, the `session.status`/`session.idle`/`session.compacted`/`session.error`/`message.updated` events exist, and `server.ts` listens on TCP only. All match the fact sheet.
- **Hygiene:** no install-specific names in the diff (`Owner: joseph` is the standard plan field, used in 112 plans).
- **Other files:** the README row, the update-versions skill edit and the meta-deploy entry (with its description) are all correct.

**Round 1**

- FIX-FIRST 1 (LXC is Plan 00209 Task 3.3): fixed at `PLAN.md:81-84` and `design.md:278`.
- FIX-FIRST 2 (cross-run resume): fixed at `design.md:77-83` and table row `:154`, with `conversation.json` as the hint and `session.json` kept for the live run.
- NOTEs 3-8: all addressed (upgrade wording, the regex cleanup, `Agent` naming, refuse → `wait`, client goroutine, port rule, criterion ticked).

**Findings**

1. **FIX-FIRST**, `research/opencode-bus-member-design.md:112-113`. The design says "A `prompt_async` failure arrives as a `session.error` event: the delivery is `missing` at once." Upstream does not work that way. In `server/routes/instance/httpapi/handlers/session.ts:311-327`, `promptAsync` forks the whole `promptSvc.prompt(...)`, which stores the user message and then runs the turn. `catchCause` publishes `Session.Event.Error` for any failure in that, including a provider or model error after the message is already stored. The event also carries only `sessionID`: no message id, no nonce.

   - **Result:** a stored delivery would be declared `missing`, and v2 B 2.7.3 then returns its items to `pending` with `repeat`, so they are delivered twice. With several `awaiting` entries, the error cannot be tied to any one of them.
   - **Fix:** a `session.error` only prompts a check. Look for each awaiting entry's needle in `GET /session/:id/message`. Found means `proven`. Only an entry whose needle is absent after the error may be declared `missing`. Add to H-OC3 that a turn error after the message is stored does not cause a redelivery.

2. **NOTE**, `design.md:66-69`. Launch failure in B 2.4.1 step 7 is defined only "under `--claude`", in terms of the first SessionStart. Fix: say what counts as an opencode launch failure (CMD exits, or `/global/health` does not answer within 30 s, means `launch-failed`, exit 78). Add "a busy `--port` ends `opencode serve`" to H-OC1, since that rule depends on it.

3. **NOTE**, `design.md:59-64`. `resolveNetworkOptions` (`cli/network.ts`) appends the user's global `server.cors` origins even when `--cors` is not given. Refusing `--cors` on CMD therefore does not fix the CORS list. Fix: say the worker's global config is provisioned without `server.*`, or add it to H-OC5.

4. **NOTE**, `design.md:84-87`. `refuse` → `wait` "until the first join relaunches the session" copies a limitation that only Claude Code has: it reads `crossSessionInbound` once, at launch. opencode has no setting fixed at launch, so the link could start pushing on the first join without a relaunch. Fix: state this as a choice (keep it for parity, or drop the relaunch for opencode) rather than "exactly as for Claude Code".

5. **NOTE**, `design.md:78-83`. v2's `conversation.json` schema (B 2.3.3) has `"session_id": "<claude session id>"`, and area C's relaunch uses it as `claude --resume <id>` (C9). Fix: add the agent (`launch`) to the record and resume only when it matches, so an opencode id is never given to `claude --resume`. Name C9's relaunch for an opencode member: it is a session reused through the API, not a CMD argument.

I changed no tracked file. The scratch copies of upstream sources and v2 docs that I fetched into `untracked/scratch/rv2/` have been deleted.
