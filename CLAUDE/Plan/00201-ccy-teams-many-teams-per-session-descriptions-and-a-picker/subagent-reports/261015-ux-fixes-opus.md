# Plan 00201 Task 2.10: UX fixes before the first day with a human in the loop

Branch `fix/teams-ux`, from `origin/feat/bus-inject-message` (7f76372a) with local F44
(446634e8, the `agent-bus delivery` docs wording fix) merged in. Input: the Opus UX review
(untracked scratch). Tests first for every code change, each seen red, then green.

## Per item

1. **Silent drops made visible.** `pingbus watch` sends a public `m.read` receipt
   (`matrix.Client.read_receipt`) for each human message it delivered, after the spool lock is
   released; a refused receipt is one stderr line and the delivery stands. Element shows the
   agent's avatar under a message that reached it, and none under one that reached no agent.
   `docs/agent-bus.md` has a new section, "Talking to agents from Element": only a message that
   mentions an agent picked from the `@` list reaches it, edits are ignored, Reply is fine,
   answers are replies, about 2,000 characters are shown.
   Tests: `test_matrix.ReadReceiptTest`, `test_notify` (human gets a receipt, peer none,
   refused receipt said and the delivery not repeated); the fake homeserver models the
   endpoint.
   **Proposal, not built:** telling the human in the room that an unmentioned message reached
   no agent. The only safe, simple form is a fixed `say` from the team coordinator seat's
   watcher (not the model, so no turn is spent) to the human, at most once per human per 10
   minutes: "Your message mentioned no agent, so none received it: pick the agent from the @
   list." Not built because humans also talk to each other in the room (each such message
   would be answered by a bot), it needs a coordinator, and the receipts already show the
   difference. Worth revisiting if the owner misses receipts in practice.
2. **Threading.** `pingbus say --re <event>` sets `m.relates_to.m.in_reply_to`; receivers
   accept exactly that shape (`m.replace` stays `edit`, any other shape `schema`), protocol §7
   updated. `REPLY` (so the envelope's `reply:`) is `pingbus --team T say --to N --re '<id>'`.
   The event ID is single-quoted: unquoted, a shell expands its leading `$` to nothing. The
   ping reply line had the same flaw (`--re $id`) and is quoted too. **A receiver on the old
   pingbus drops a reply as `edit`**, so every member must be on this release (it ships to
   ccy, `cc` and LXC together through the two queued plays).
3. **ACCEPTANCE-owner.md** rewritten: a "Morning start" at the top (deploy, relaunch, update
   teams, Element sign-in per team, start agents, coordinator, talking, what to do when
   something is off), then the checks with the review's corrections (prerequisites listed;
   step 03 measures homeserver restarts; step 04 says "Keep as is"; step 05 uses `! pingbus status`; step 07 checks scope with `ccy-teams for-checkout`; step 08 says how to re-test
   consent; step 09 needs no session ID), and the 00194 H1-H7 checks (H7 optional).
4. **Launch refusal.** `agent-bus seat check/take/remove` for a team whose homeserver is not
   active: built here (an installer stamp in `/var/lib/agent-bus-applied`) gives "is built on
   this host but its homeserver is not running ... ccy-teams apply <team> starts it"; otherwise
   "there is no team `<x>` on this host ...; teams here: a, b (or none yet). Check the name, or
   ccy-teams create makes one". "Declared" is read as "stamped": checkout.py runs from the
   installed zipapp and does not know the checkout's host_vars; a declared-but-never-built team
   gets the second message, which still points at the right tools.
5. **One window per team.** Getting started step 2 says each team is its own Element window
   and why, linking RESEARCH-one-element-window.md. `ccy-teams`: Enter on a team row has
   **Open every team in Element, a window each** (no shortcut; it opens every team with a
   profile and names those without one). Help text and usage updated.
6. **Readable bodies.** `envelope._render` shows printable non-ASCII letters, digits, marks,
   punctuation and symbols as themselves and escapes: C0/C1/DEL, every Cf (bidi embeddings,
   overrides and isolates, LRM/RLM/ALM, zero-width, word joiner, BOM, soft hyphen, tag
   characters), Zs other than U+0020, Zl, Zp, surrogates, private use, unassigned; variation
   selectors except one U+FE0E/U+FE0F straight after a symbol (so a run, the "emoji smuggling"
   carrier, is escaped); a combining mark not after a letter, digit or shown character (so none
   can sit on the envelope's own quote); invisible fillers (Hangul fillers, braille blank, CGJ,
   Khmer and Mongolian invisibles); anything whose NFKC form contains `"`, `\`, `<`, `>` or a
   control (fullwidth and small forms); and the look-alikes NFKC leaves alone (curly and other
   double quotes, guillemets, single angle quotes, ⟨⟩⟪⟫〈〉《》≪≫˂˃ᐸᐳ, ∖ and kin). Single curly
   quotes and dashes are shown. The body cap is 2048 UTF-8 bytes; the 12 KiB delivery cap is
   measured on the bytes the socket carries (`notify` sends an ASCII JSON message, so a
   non-ASCII character counts as its escape); the largest-entry ceiling is 9 KiB. The SPEC-inject
   attack set (3.5 forgery, every hostile character) passes in five positions each, plus the
   string-cannot-end-early cases. SPEC-inject.md E2, E3, E6, E8, E9 and T3 amended.

## For the owner

- **Not changed (as instructed):** a brand-new seat cannot be made coordinator at launch; it
  joins as a worker, and Make coordinator is on its row in `ccy-teams` (or in the chooser next
  launch). The Morning start says so.
- Every team will read "out of date: software" after `play-agent-bus.yml`; `ccy-teams` offers
  the update.
- H4 still has to measure the socket's largest accepted message before the caps are frozen.

## Versions and deploy

CCY 3.98.9, container 2.64, plugin 1.2.1, changelog entry. No new queue entries:
`play-claude-yolo.yml` (launcher, image, `ccy-teams`), `play-agent-bus.yml` (host pingbus, kit,
and the `agent-bus` seat commands with the new refusals), and the 00161 and 00201 acceptances
are already queued; their descriptions in `meta-deploy.list` now say what this branch adds.

## QA

Targeted: every `tests/helpers/pingbus` module, `test_checkout`, `test_member_docs`,
`test_protocol_doc`, `test_plugin_contract`, `go test ./internal/teams/...`, ruff, and
`qa-changed.bash` on the branch.
