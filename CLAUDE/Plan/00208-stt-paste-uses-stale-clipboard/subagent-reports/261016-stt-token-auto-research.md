# Research: post-process with the Claude token that has the most allowance left

Plan 00208 Phase 4, research before building. The owner's request (speech): a box to tick
that uses the token with the most usage allowance, so tokens need not be switched by hand;
and when every token is out, fail fast and show a warning on the panel icon.

Background, proven on the host: a Ctrl+Insert failed because the token named in the
`claude-token` setting had hit its weekly limit (`claude -p` exit 1, "You've hit your weekly
limit · resets ..."). Notifications being off hid it; Phase 3 (Task 3.1) fixed the hiding.

## Q1. How tokens are stored and chosen today

- **The store** is `~/.claude-tokens/ccy/tokens/NAME.YYYY-MM-DD.token`, the date being the
  expiry recorded at creation (`ccy --create-token`, `claude setup-token` underneath). One
  long-lived `sk-ant-oat01-` OAuth token per file. Help text:
  `files/var/local/claude-yolo/claude-yolo:305-309`.
- **ccy** launches a name with the **earliest-dated** file `NAME.*.token` matches, and warns
  when later files of the name exist (`token_shadow_warning`,
  `files/var/local/claude-yolo/lib/token-management.bash:110-133`; the report's statement of
  the rule at `:670-673`).
- **cc** sources the same library and the same `select_token` chooser in `host` mode, with a
  Desktop pseudo-option (`files/var/local/claude-code/cc:218-236`; Plan 00048).
- **Speech-to-text**: the setting `claude-token` (schema
  `extensions/speech-to-text@fedora-desktop/schemas/org.gnome.shell.extensions.speech-to-text.gschema.xml:79-83`)
  holds a name, chosen in Settings from the names in the store
  (`prefs.js` `_listClaudeTokens`, around line 303). `wsi-claude-process` reads it through
  `wsi-setting` and uses the **newest unexpired** file of the name (`export_named_token`),
  then runs `claude -p --model <m>` with `CLAUDE_CODE_OAUTH_TOKEN` exported. Empty means the
  desktop login.
- **A difference already present**: for a name with two files, ccy uses the earliest-dated
  and speech-to-text the newest unexpired. Not changed here for the named token (that would
  change what the owner's existing setting means); the automatic choice below uses the file
  ccy reads the figures with, so the figures and the credential always belong together.
- Callers of `wsi-claude-process`: `wsi` (Go, `src-go/internal/wsi/transcribe.go:153-170`),
  `wsi-stream` (Python, line ~893), `wsi-postprocess` (Go, `src-go/internal/wsipost/app.go`),
  `wsi-article-window` (Python). All show its stderr as the failure reason.

## Q2. Is there a trustworthy, non-scraping measure of each token's remaining allowance?

**Yes, one, and it is already built and tested: `ccy --token-usage --json`.**

- What it is: Plan 00101 (Completed). A setup-token's 5-hour and 7-day utilisation arrive as
  `anthropic-ratelimit-unified-*` **response headers** on `POST /v1/messages`, which the
  token's scope can reach. ccy sends one Haiku request (`max_tokens: 1`, one character) per
  token and reads the headers (`token-management.bash:434-477`, `_usage_extract` at
  `:308-348`). Measured on the host: F13 (200 with the full header set), F17/F20
  (utilisation is a fraction; the bundle itself computes `floor(utilization*100)`), and a
  field sample of 1.01 on a bucket that had just hit its limit (`token-management.bash:14-20`).
- The script-facing contract: `ccy --token-usage --json`, schema 1, documented in
  `ccy --help` (`claude-yolo:311-336`): per token `name`, `file`, `status` (`ok` or
  `unavailable` with a `reason`), `five_hour_pct`, `seven_day_pct` (0-100, **over 100 means
  past the limit**), `resets.five_hour` / `resets.seven_day` (ISO-8601 UTC). It needs no
  terminal, repository or container (`claude-yolo:414-422`), never prints a token value, and
  is pinned by `scripts/test-ccy-token-usage-command.bash`. Reading it goes through ccy's own
  token path, so this feature never opens a token file to measure it.
- **Cost**: one small billed request per token, unless read in the last 15 minutes
  (`CCY_USAGE_TTL=900`, `token-management.bash:156-158`). With the box ticked, every
  post-processing more than 15 minutes after the last reads every token once.
- **Caveats, stated rather than hidden**:
  - F37: the header family is undocumented and may change without notice. ccy already turns
    a value its scale cannot produce into a warning and a null, never a number
    (`_usage_report_pct`), so a contract change surfaces as "no usable figures", which this
    feature treats as unusable, not as headroom.
  - F21: there is no per-model weekly utilisation header (`seven_day_opus` has none). The
    7-day figure is the account's overall weekly allowance. The host failure said "weekly
    limit", which is that bucket, but an Opus-only sub-limit could refuse a token whose
    7-day figure is below 100%.
  - Figures may be up to 15 minutes old. The token chosen is the one with the most room, so
    one that was at 99% a quarter of an hour ago is never the one chosen while another has
    room.
- **Rejected sources**:
  - `GET /api/oauth/usage`: 403 for every setup-token, scope `user:profile` missing, fixed at
    minting (Plan 00100; F9; `token-management.bash:57-63`).
  - The hooks daemon's usage snapshot (`usage-pause`, the status line): it is the
    `rate_limits` of the Status event of the session the daemon serves, one account-wide
    snapshot, not keyed by token (`.claude/hooks-daemon/src/claude_code_hooks_daemon/core/usage_snapshot.py:1-33`).
    It cannot rank stored tokens.
  - `claude -p`: has no path that prints the headers (F12, F14).
  - The CLI's "You've hit your weekly limit" text: human wording, not a contract. Reading it
    is scraping.

## Q3. Rotation on a "limit reached" error: recommended against

The measure exists, so no rotation is needed to choose well. The choice is a **decision rule
on measured figures**: rank every token by its room (100 less its higher reported
percentage); choose the most; if none has room, stop. Nothing is tried and then retried.

Rotating after `claude -p` fails would need the failure to be recognised as a limit, and the
only signal is the CLI's English message. A misread (a network error, a logged-out token, a
changed wording) would then send the same text to the next account, spend its allowance and
replace the real reason with a later one: the error-hiding chain the owner forbids. So a
failed run is reported as it is, naming the token it ran as, and the next run chooses again
from figures that by then show the limit.

If the owner wants one more step, the honest form is not a message match but a re-measure:
after a failed run, read `ccy --token-usage` afresh (bypassing the 15-minute cache, which
ccy does not yet offer), and choose again only if the figures now show the chosen token at or
past 100%. That is still a decision on measured data, bounded to one, and it is a decision
for the owner (D3 below), not built here.

## Design built (Phase 4)

- **Setting** `claude-token-auto` (boolean, default off). Settings: a switch "Use the token
  with the most allowance left" beside the token choice, its subtitle stating the cost; the
  named-token row is greyed out while it is on.
- **`wsi-claude-token`** (Go, `src-go/internal/wsitoken`): the one place a post-processing
  account is chosen. Prints the token file to use, or nothing for the desktop login.
  `wsi-claude-process` (bash, on the ratchet allowlist, so it may only shrink) calls it in
  place of its own token code, which moves to Go, and names the account in its debug log and
  in its failure reason.
- **All exhausted or unreadable**: no paste (the caller gets a failure), the reason names
  every token's state and its reset time, through the persistent failure notice Task 3.1
  built, and a state file `~/.local/share/speech-to-text/claude-tokens-exhausted` holding that
  text. The panel shows a warning icon beside the microphone while it exists, and a menu item
  with the reason; clicking it clears the warning. The next automatic choice that finds a
  token with room removes the file.

## Decisions for the owner

- **D1. Automatic reading of usage.** Plan 00101 Decision 1 was "human-triggered, never
  automatic", because each read spends allowance. Ticking the box is taken here as that
  human choice: the cost is in the switch's own subtitle. Confirm.
- **D2. Which file of a name.** Automatic mode uses ccy's file (earliest-dated); the named
  setting keeps the newest unexpired. Align them (and which way), or keep?
- **D3. Re-measure after a failure** (Q3's last paragraph): wanted, or not?
- **D4. Opus-only weekly limit** (F21): accept that the measure is the overall weekly
  allowance?
- **D5. Success notification.** The chosen token is named in the debug log and in every
  failure; a successful run's notification (shown only with Show Notifications on) does not
  name it, since that text is built in four callers. Wanted?
