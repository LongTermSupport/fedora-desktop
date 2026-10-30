# Plan 00241: agent bus message style

**Status**: In Progress
**Created**: 2026-10-30
**Owner**: joseph
**Priority**: High

## Overview

Agents' messages are hard to read in Element. pingbus sends only a plain `body`, so
Element shows raw text, and team notes and reports go out as `m.notice`, which Element
draws dimmed. Matrix can carry formatting: alongside the plain body, a message may carry
HTML (`format: org.matrix.custom.html`, `formatted_body`), which Element renders. Element's
own composer turns the markdown a person types into that HTML before sending.

The owner's decisions (2026-10-30):

- On the human-facing channels (reports, questions and their threads, and anything
  human-facing added later), agents write markdown and pingbus converts it. There is one
  style, with a fixed headline emoji for each kind of message.
- Between agents (`pingbus say`), messages stay plain text, in a terse line protocol
  optimised for tokens; a person can read it, but that comes second.

[STYLE.md](STYLE.md) holds both styles.

No receiver checks the keys of a human-facing message exactly. `check_report_content` has
no caller outside its tests, and `parse_question` reads only its own key. So a member on
the current kit shows the plain `body` of a formatted message and refuses nothing, and one
deploy is enough. Agent text (`validate_text_content`, which is exact) does not change.

## Goals

- Every message a person reads in Element is formatted: headings, bullets, bold, italics,
  blockquotes, code.
- Agents' messages to each other are short and uniform, and cost fewer tokens.
- Both styles are in STYLE.md, which the pingbus skill points every agent to.
- No member refuses a message during the rollout.

## Non-Goals

- Converting what agents receive. The envelope keeps the markdown text, which agents read
  as it is.
- The v2 bus (Plan 00209), which carries the converter and the style over when it ports
  the senders.

## Tasks

### Phase 1: The style

- [x] ✅ **Task 1.1**: STYLE.md. For people: the headline grammar, the nine kinds and
  their emoji, the item markers, the body rules, the length limit and an example. Between
  agents: the line protocol (section 6). The owner, 2026-10-30: "let's launch with it and we
  can always fine tune it later ... keeping it open ended so we can version it". Launched as
  version 1; a change to it bumps the version and says what changed.

### Phase 2: Senders convert

- [x] ✅ **Task 2.1**: `helpers/pingbus/markdown.py`, a stdlib-only converter of the
  STYLE.md subset to Matrix's allowed HTML. It covers headings, paragraphs, `-`, `*` and
  `1.` lists, `>` quotes, bold, italics, inline code, fenced code and links. Everything is
  HTML-escaped first. Test first (`tests/helpers/pingbus/test_markdown.py`).
- [x] ✅ **Task 2.2**: `report`, `ask-human` and the thread posts send `formatted_body`
  (`check_report_content` accepts the pair, and refuses one key without the other);
  `say` stays plain. Reports and quiet thread posts stay `m.notice`. Matrix's default push
  rules suppress notices (`.m.rule.suppress_notices`), and an `m.text` would notify every
  member through `.m.rule.message`, which a report must never do. Element draws a notice a
  little dimmed. The owner can trade that against notifications.
- [x] ✅ **Task 2.3**: The pingbus skill gives both styles: markdown for reports and
  questions, and the line protocol for `say`.
- [ ] 🔄 **Task 2.4**: The deploy: play-agent-bus for the member kit, and play-claude-yolo
  for the kit in the ccy image (CCY 3.104.4, container 2.79.17, which means one image
  rebuild). Then the owner reads a message of each kind in Element. Deployed on the host
  at be1611132 (2026-10-30, both plays passed). The host agent posted the first formatted
  report (`$WApOGs6ePK8RScdlyJOM8ePNT_Hl223gW25vJ1ui-zk`) and found the markdown formatter
  rewrites a `.md` draft before it is sent, so STYLE.md now says to draft in `.txt`. Left:
  the owner's verdict in Element; sessions on an older ccy image send plain text until they
  restart.

## Success Criteria

- [ ] The owner finds the team room readable (their verdict in Element).
- [ ] No refusal of a well-formed message by any member during the deploy.

## Delivery & Milestones

- <!-- milestone or delivery commit hash -->
