# Documentation Strategy — one canonical home per fact

> **This file is DAEMON-OWNED.** It is deployed to
> `CLAUDE/core/DocumentationStrategy.core.md` and overwritten wholesale on
> every install and upgrade, so a local edit is discarded the next time the
> daemon deploys — never hand-edit it. Your own
> `CLAUDE/DocumentationStrategy.md` is seeded once, is never touched again,
> and opens with a link to this file. Every project-specific ruling — an
> exception this project grants, a tree it keeps outside the model, the
> adoption state of each check — belongs there, not here.
>
> Wrapper paths below are written as `.claude/hooks-daemon/bin/hooks-daemon`,
> the location for a standard install. A self-install checkout, where the
> daemon root IS the project root, uses `bin/hooks-daemon` at the project root
> instead; substitute accordingly.
>
> Directory names below are written at their **defaults** — `CLAUDE/` for the
> agent tree, `docs/` for the human tree, `remote-docs/` for the vendored
> tree. All three are per-project configuration (`documentation.trees.agent`,
> `documentation.trees.human`, `documentation.trees.remote`). Read
> `.claude/hooks-daemon.yaml` for the values this project actually uses and
> substitute them as you read: the NAMES are yours to choose, the MODEL is
> not.

This document defines the documentation single-source-of-truth (SSoT) ruleset
supported and enforced by the Claude Code Hooks Daemon's `documentation`
configuration. Developers and AI agents writing, moving or reviewing
documentation in a project with that configuration enabled should follow it.

The ruleset is binding as **policy** whether or not enforcement is switched
on. The deterministic checks catch a mechanically-checkable subset of it, not
the whole thing — a passing check run is not a compliant corpus, and an
unenforced project is not an exempt one.

---

## Why this exists

Documentation does not usually fail by being wrong when it is written. It
fails by being **copied**, and then by one copy being corrected while the
others are not.

The failure has a specific shape, and it is worth recognising because it
looks harmless at every individual step:

1. A fact is written in its proper home.
2. Someone working on a different surface — a rules file, a skill, an agent
   definition, a sub-folder `CLAUDE.md`, a PR body, a code comment — needs
   the reader to know that fact, so they restate it there. Reasonably: a
   link would have cost the reader a hop.
3. The fact changes. The person changing it updates the home, because that
   is the file they were in.
4. Every copy is now wrong, and nothing anywhere says so.
5. A reader arrives at a copy. It is confident, well-written and stale. They
   have no way to tell, because the copy does not know it is a copy.

The cost is not the duplication. The cost is that **correcting the fact
becomes a graph traversal nobody performs**, and that agents in particular
will act on whichever copy they read first. Every rule below is a defence
against one step of that sequence.

The second failure mode is quieter: a fact with **no** home at all, scattered
in fragments across five surfaces, none of them authoritative. Nobody can
correct it because there is nothing to correct. R1's "exactly one" cuts both
ways — not two homes, and not zero.

---

## The model

### The trees

There are exactly **two authored documentation trees**, split by AUDIENCE,
plus a third tree that is captured rather than authored:

| Tree                                                                   | Audience | Register                                                                                                      |
| ---------------------------------------------------------------------- | -------- | ------------------------------------------------------------------------------------------------------------- |
| **Agent tree** — `documentation.trees.agent`, default `CLAUDE/`        | agents   | Verbose, information-dense, exhaustive. **OWNS the depth for every fact.**                                    |
| **Human tree** — `documentation.trees.human`, default `docs/`          | humans   | Terse, digestible, friendly prose. MAY point into the agent tree for full depth; never restates it at length. |
| **Remote tree** — `documentation.trees.remote`, default `remote-docs/` | both     | Vendored upstream documents, captured verbatim with provenance frontmatter. Not ours to rewrite; see R2b.     |

The agent tree owns the depth. That is the load-bearing sentence in the
table. When a human-tree page and an agent-tree page disagree, the agent tree
is right by construction, and the human page is the defect.

Humans may of course read the agent tree — but should expect its register:
long, dense, and written for a reader with no patience and no prior context.

The plan directory (`plan_workflow.directory`, default `CLAUDE/Plan/`) is a
subdirectory of the agent tree, because the plan system is primarily for
agents. Its contents are governed by R8.

### Everything else is a satellite

Every other markdown surface in the repository —

- `.claude/rules/*.md`
- `.claude/skills/**`
- `.claude/agents/*.md`
- sub-folder `CLAUDE.md` files
- code comments
- GitHub issue and pull-request bodies
- plan folders

— **POINTS at canonical docs. It never duplicates them.** Each satellite has
its own contract under R7; all of them share that one sentence.

### The split is not optional; only the names are

Tree NAMES are per-project configuration. The SPLIT itself is not: with
`documentation.enabled: true` the daemon enforces it. A project may call its
agent tree `agent-docs/` and its human tree `handbook/`; a project may not
decide it would rather have one tree serving both audiences, because the two
registers are genuinely incompatible — the shape that serves an agent
(exhaustive, repetitive, explicit about edge cases) is the shape that loses a
human reader, and the shape that serves a human (short, selective, assumed
context) is the shape that makes an agent guess.

---

## The rules

### R1 — One canonical home per fact

Every durable fact has **exactly one file that owns it**. Every other
statement of that fact anywhere in the repository is either a pointer (R4) or
a tracked quote (R4b).

**Why.** N copies makes every change a graph traversal nobody performs. The
copies never fail loudly; they diverge silently, and a reader who lands on a
stale one has no signal that a fresher one exists.

"Durable" is doing work in that sentence. A fact is durable if a reader six
months from now would still need it and would be misled by a stale version of
it. A one-off observation in a PR comment is not durable. A rule, a command,
a threshold, a contract, an invariant, a roster: durable.

### R2 — The canonical agent tree is configured, defaulting to `CLAUDE/`

Durable agent-facing truth lives in the agent tree, in clearly-named files
with logical sections. Facts about a specific code module MAY instead live in
that module's **registered** sub-`CLAUDE.md` (R7d), which then IS the
canonical home for them — a registered module doc is a first-class canonical
home, not a satellite.

**Why.** A fact's home has to be findable by someone who does not already
know it exists. "Clearly-named file, logical sections" is not style advice:
an anchor is what a pointer (R4) and a tracked quote (R4b) both address, so
an unstructured document cannot be pointed INTO, only AT.

### R2b — The remote tree is captured, not authored

`documentation.trees.remote` (default `remote-docs/`) holds documents
captured from upstream sources and stored locally so the project can read and
grep them offline. It is a distinct tree, and distinct in a way that matters
to every rule below:

- **It is not a canonical home for OUR facts.** Its documents are somebody
  else's; the SSoT rules govern what WE write about them.
- **Its contents are not duplication.** A vendored copy is a capture with
  recorded provenance, not a restatement — the same logic that exempts
  generated docs under R10.
- **It is not the human tree.** Verbatim upstream prose is the opposite of
  the terse, summarising register R3 requires of `docs/`, which is precisely
  why it is a top-level tree of its own rather than a subdirectory of the
  human tree.
- **It is never hand-edited to "improve" it.** A capture that no longer
  matches upstream is refreshed, not corrected in place.

If this project vendors upstream documentation, its capture command,
provenance schema and freshness policy are the remote-docs subsystem's
concern, not this document's.

### R3 — Audience split

A human-tree file that restates agent-tree content at length is a violation.
The intended shape is **summary plus link**. The agent tree always owns the
depth; only the tree names are configurable.

**Why.** Two documents covering the same ground in two registers are two
copies, and R1 applies to them like anything else. The asymmetry (human tree
points at agent tree, never the reverse) exists so there is always an obvious
answer to "which one do I update?" — you update the depth, and the summary
either still holds or is visibly wrong.

### R4 — The pointer test

This is the rule you will apply most often, so it is stated as a test rather
than a principle.

**A compliant pointer MAY contain:**

- **(a)** up to roughly three sentences of orientation prose, saying what the
  target covers and why this reader would follow it;
- **(b)** surface-specific APPLICATION notes that are not stated in the
  target — how this role, path or context uses the fact;
- **(c)** the link.

**A compliant pointer MAY NOT contain:**

- copied or reconstructed **tables**;
- **fenced command or code blocks** that appear in the target (unless quoted
  per R4b);
- **numbered procedures**;
- **enumerated lists** duplicating the target's;
- restated **normative sentences** carrying the target's rule;
- **derived facts** (R5).

**The boundary is CONTENT CLASS, not sentence count.** This is the part that
gets misread. A pointer is not "short"; a pointer is "structurally
non-duplicating". Three paragraphs of orientation prose can be compliant; one
copied three-row table is not.

**Why that boundary.** Structured facts are what drifts harmfully. A table, a
command, a numbered procedure or a threshold is copied EXACTLY and then
diverges exactly — the copy stays plausible while becoming wrong, and a
reader executes it. Prose paraphrase mostly does not fail that way: it ages
into vagueness rather than into confident falsehood, and a reader who needs
precision follows the link. So the rule spends its strictness where the
damage is.

### R4a — Safety-critical restatement exception

A rule whose LOSS is dangerous — a release human-gating step, a destructive
command that must never be run unattended, a security boundary — may be
restated in compressed prose at the point of use, even though R4 would
otherwise forbid it.

The exception is narrow and comes with three conditions:

1. It must **cite the canonical doc**.
2. It still may not restate that doc's **tables, steps or counts** — the
   compressed prose carries the WARNING, not the procedure.
3. It must **claim the exception in-file with an HTML comment marker**, so
   the restatement is auditable by a plain grep rather than being
   indistinguishable from an ordinary R4 violation.

**Why.** R4 optimises for a corpus that stays correct. A safety rule
optimises for a reader who never reaches the link — and a reader who is about
to do something irreversible is exactly the reader who did not follow it. The
marker exists so that this trade is made deliberately and once, rather than
becoming a general-purpose excuse; there is no deterministic check behind it,
which is precisely why it must be greppable. If this project has settled on a
marker spelling, its own `CLAUDE/DocumentationStrategy.md` records it.

### R4b — The `ssot-quote` mechanism

A small verbatim excerpt MAY be repeated anywhere IF it is wrapped in
metadata naming its source (file plus a heading or marker anchor):

```markdown
<!-- ssot-quote: CLAUDE/SomeDoc.md#some-anchor -->
(verbatim excerpt)
<!-- /ssot-quote -->
```

This turns deliberate repetition into **tracked quotation**. The checker
verifies each quote against its source span — normalised for formatting, so
two equivalent renderings never register as drift — and reports the moment
the canonical copy changes. A quote is a copy that KNOWS it is a copy, which
is the entire difference.

Use it where inline text genuinely serves the reader better than a link: a
short verification snippet repeated at several points of use, a one-sentence
rule that must be visible at the moment of action.

Practical constraints worth knowing before you reach for it:

- **Anchors are headings or explicit markers, never line numbers.** Line
  numbers rot on the next edit to the file above them. An explicit
  `<!-- ssot-anchor: name -->` in the SOURCE document is preferred over a
  heading slug for anything whose heading carries punctuation, emoji or a
  colon, because heading-to-slug algorithms disagree on exactly those.
- **A quote must be long enough to be meaningful.** The verifier enforces a
  minimum length (80 normalised characters as shipped) because a one-line
  quote is a substring of almost anything: it would verify trivially and
  protect nothing.
- **A quote may not span two sections.** Verification requires the body to be
  a contiguous substring of ONE anchor's span, so text crossing a heading
  boundary fails by construction. Split it into two `ssot-quote` blocks
  against two anchors.
- **Quote, do not paraphrase-and-tag.** The mechanism verifies text, not
  intent. A "quote" that has been lightly reworded is simply a quote that is
  already drifted.

### R5 — Derived facts are stated only by their source

Counts, enumerated lists that have a generator, step totals, version numbers,
file or handler rosters: stated **only** in the file, script or registry that
produces them, or in a generated doc (R10). Everywhere else the correct text
is "see X, the single source of truth for the list".

**Why.** A derived fact is a copy with an expiry date nobody sets. "There are
fourteen checks" is true on the day it is written and false at the next
commit, and unlike a prose claim it is falsified by work that had no reason
to touch the document. This rule is the one projects most reliably invent for
themselves during incident response — twice, usually, before writing it
down — because a wrong count is the failure that survives review: it reads as
precision.

The tell: if you can imagine a script that computes the sentence, do not
write the sentence.

### R6 — Links are plain and resolve

Use root-relative (or verified-relative) markdown links. No case-mismatched
paths. No links to files or anchors that do not exist.

**No `@`-imports outside the deliberate resident set in root `CLAUDE.md`.**

**Why the `@`-import prohibition specifically.** An `@`-import is not a link;
it is an instruction to **re-inline the whole target eagerly**, every session,
whether or not the reader needs it. That defeats progressive disclosure — the
entire reason a corpus is split into files a reader loads on demand. A tree
of `@`-imports collapses back into one enormous always-loaded document, and
the context it consumes is charged to every task, including the ones with
nothing to do with the imported subject.

The allowlist is `documentation.qa.resident_at_imports` (default
`["CLAUDE.md"]`). Extend it **only** for a file that is genuinely, deliberately
always-loaded. A document that is read on demand, when an agent is told to
read it — a core document like this one included — is not resident, and
adding it to the allowlist to silence a check would be declaring a file
resident in order to make a correct finding go away.

Quoting an `@`-import while writing ABOUT the rule is fine: occurrences
inside backticks or fenced blocks are not treated as imports.

### R7 — Satellite surface contracts

Each satellite has a contract. They differ, but they share R1.

#### R7a — `.claude/rules/*.md`: POINTERS ONLY (firm)

The whole permitted shape: frontmatter (`paths:` plus `description`), a
trigger statement, the rule in **at most two imperative lines**, and link(s)
to the agent tree or a registered `CLAUDE.md`.

**No fences. No tables. No numbered procedures. No quotes.** Plus a small
body budget (15 non-blank, non-frontmatter lines as shipped — the finding
message always names the operative number).

If the substance has no canonical home yet: **CREATE THE HOME FIRST**, then
thin the rules file to a pointer.

**The transition rule matters as much as the shape.** A rules file may not be
thinned by DELETING its content. Promote the content to the canonical home —
or verify it is already there — and only then thin. Otherwise the cleanup
that was supposed to remove a duplicate removes the only copy, and the
commit looks like compliance.

**Why so strict here.** Rules files are injected into context by path glob,
which means they are read constantly, by agents, without being asked for. A
rules file that carries substance is a copy that gets read more often than
its own canonical home — the worst possible place for drift to live.

#### R7b — `.claude/skills/`

Thin, intent-matched shims that POINT. A skill's own invocation
mechanics — what it is triggered by, what arguments it takes, what it
dispatches — are legitimately its own content. **Procedure bodies are not**:
if the skill tells the reader how to do the work, that "how" belongs in a
canonical doc the skill links to.

If the project keeps a charter `CLAUDE.md` in its skills directory, that
charter is the canonical home for what a skill may contain.

#### R7c — `.claude/agents/*.md`

Role framing, plus role-unique behavioural instruction, plus pointers.

**Not** engineering-principle restatements, **not** code skeletons, **not**
remediation cookbooks. An agent definition says who the agent is and what is
different about how it works; everything it needs to KNOW is a link.

A charter `CLAUDE.md` in the agents directory mirrors the skills one.

#### R7d — Sub-folder `CLAUDE.md`

A sub-folder `CLAUDE.md` is one of exactly two things:

1. a **pure routing table** (roughly 15 lines: "for X see Y"), or
2. a **registered module-local canonical home**, declared in
   `documentation.qa.registered_module_docs`.

**The outside-reader test decides the content.** Content belongs in a module
doc if and only if its intended reader is an agent about to edit files in
THAT subtree, and the content is meaningless without the subtree.

**Qualifying:** edit guards ("do not hand-edit these, they are generated");
module-local invariants and concurrency contracts; local build and test
commands; editing gotchas; local conformance walkthroughs; a tiny generated
index.

**Disqualifying:** repo-wide mandates; procedures for actors elsewhere;
restated derived facts (R5); hand-written API mirrors of the module's own
code; governance or engineering-principle recaps.

**Registration is a config act, not a formatting one.** An unregistered
module doc carries a routing budget (around 40 lines, advisory). A registered
one gets generous size tiers with grow-only blocking. Registering says "this
file IS a canonical home", with the responsibility that implies.

**Prose indexes with per-file descriptions are forbidden** — generate them or
delete them. They rot fastest of anything in a repository, because every file
added or renamed falsifies them and nothing enforces the update.

#### R7e — Code comments

A comment carries **current-state rationale local to the code**. Durable
knowledge a reader OUTSIDE the file would need goes into a doc, with the
comment pointing at it.

**A verbose comment block IS documentation**, and is cross-checked as such —
a forty-line comment explaining a subsystem is an agent-tree document that
happens to be stored where no reader will find it and no doc check will
govern it.

There is deliberately **no deterministic "knowledge-in-comment" blocker**:
the discriminator is semantic (is this rationale for THIS code, or is it a
subsystem explanation?), and a regex cannot make that call. The mechanical
backstops that do exist — comment-changelog and comment-size guards — catch
narrower, mechanically-decidable failures, not this one.

#### R7f — GitHub issues and pull-request bodies

Point, do not restate. Policy first; enforcement here is at most a weak
advisory on `gh` invocations.

**Why weak.** An issue body is written once and read in a context where the
canonical doc may not be reachable, and it is not part of the repository's
corpus. The rule is real, the enforcement is deliberately light, and the
practical instruction is simply: link to the doc rather than pasting its
table into the issue where it will be quoted back at you a year later.

### R8 — Plan folders are a drafting ground, not a home

Documents may be created and iterated freely inside a plan folder — that is
what a plan folder is FOR. What is forbidden is leaving them there as the
canonical home.

At terminal-status flip (the plan reaching a completed, superseded or
abandoned state), **every supporting doc in the folder gets an explicit
disposition, recorded in the closing journal entry**, and there are exactly
three:

- **promote** — the canonical content moves into the doc trees; a stub
  pointer stays behind in the plan folder so historical links still resolve.
- **historical** — the default for dated snapshots. Archive immutability
  applies: it is never retro-edited, and nothing outside cites it as current
  guidance.
- **delete** — it served the drafting and is gone.

The plan completion checklist carries this step.

**Why.** A plan folder is the most attractive place to write a document and
the worst place to keep one. It is dated by construction, it is archived when
the plan closes, and its content is written for people who are inside the
plan's context. A canonical fact that ends up there is functionally lost: the
next reader finds it only by grepping an archive they had no reason to open,
and cannot tell whether they are reading current policy or a rejected draft.

### R9 — Plan citations are provenance, not storage

A canonical doc MAY cite a plan (with a path) as history: "this rule exists
because of the incident recorded in plan NNNNN".

Current guidance the reader NEEDS may **not** exist only behind a plan
reference. If the reader must open the plan to learn the rule, the rule does
not have a home yet.

**Why.** Provenance and storage feel similar when you write them and are
completely different when someone reads them. Citing a plan enriches a
document; delegating to one hollows it out — and a plan folder is an archive
(R8), so the delegation points into content that is frozen by policy and
increasingly detached from how things now work.

### R10 — Generated docs are compliant SSoT; declare them

A document generated FROM code is generation, not duplication — its source is
the code, and it cannot drift as long as it is regenerated.

Every generated doc is **declared in a manifest**:
`documentation.qa.generated_docs`, a list of `glob` plus `generator` (the
command that regenerates it, shown to whoever trips over the file). The
manifest is pre-seeded with the daemon's own generated artefacts.

Manifest entries are **exempt from duplication checks** — of course they
duplicate; that is what generation is. A hand-edit to one draws an advisory
naming the source and the regeneration command, because a hand-edit to a
generated file is work that will be silently destroyed the next time the
generator runs.

**Why declaration is required rather than inferred.** An undeclared generated
file is indistinguishable from a hand-maintained duplicate, both to a checker
and to a human reader deciding whether to fix a typo in it. The manifest is
how the corpus knows which of its documents have a source.

### R11 — This policy obeys itself

This file is the canonical home for the ruleset. Root `CLAUDE.md` carries a
pointer plus the shortest possible resident summary. Generated handler
guidance summarises and points, and is generated — hence R10-exempt.

**Why state the obvious.** A documentation-SSoT policy that maintains its own
duplicate copies is not a policy, it is a demonstration of the problem. If
you find yourself unable to apply a rule to this document, that is evidence
about the rule.

### R12 — Grandfathering

Violations standing at adoption time go into a file-scoped config allowlist
(`documentation.qa.grandfather_allowlist`) and only ever ADVISE.

Blocking follows a strict tiering philosophy: **only an edit that makes
things WORSE can block.**

- **Worse** — a new duplicate block, a new dead link, growth of an
  over-budget surface: block-eligible.
- **Unchanged but bad** — the violation was already there and this edit did
  not touch it: advise.
- **Better** — the surface shrank, a violation was removed: **silent**, even
  if the file is still non-compliant.

**Why.** A ruleset adopted into an existing corpus finds every file
non-compliant on day one. A naive gate then blocks an unrelated typo fix in a
long-standing file, which teaches everyone that the checker is noise and must
be disabled. Worse-only means the corpus can only improve, and it means the
person fixing a typo is never punished for the corpus's history.

### R13 — Two enforcement instruments, cleanly divided

**Deterministic checks** — the edit-time handler, the commit gate, the
session sweep and the bulk CLI — enforce **only the mechanically-checkable
subset**: exact and near-exact copies, stale pointers, quote-versus-source
drift, structural placement, manifests, and budgets.

**The semantic remainder** — conflicting truths, paraphrase drift, truth
scattered with no canonical home, verbose comment blocks acting as
documentation — belongs to the **`hooks-daemon-docs-qa` agent**, which
reports with citations and **never edits and never blocks**.

**No deterministic check may attempt a semantic judgement.**

All deterministic checks ship advisory (`warn`); `block` is a per-check,
per-project ratchet.

**Why the division is absolute.** A regex that guesses at meaning produces
false positives, and a false positive in a blocking check is worse than no
check at all: it trains everyone to route around the gate, taking the true
positives with it. Conversely, a semantic reviewer that blocks would be a
non-deterministic gate — the same edit passing or failing on different runs.
So each instrument does only what it can do reliably: the checker is exact
and narrow, the agent is broad and advisory.

---

## Applying the rules

### Where does this fact go?

Work down the list; the first match wins.

1. **Is it generated from code?** Declare it in the manifest (R10) and stop.
   Do not hand-maintain it.
2. **Is it a derived fact — a count, a roster, a version, a total?** It goes
   in the thing that produces it, and nowhere else (R5).
3. **Is its reader an agent about to edit one specific subtree, and is it
   meaningless outside that subtree?** A registered module `CLAUDE.md` in
   that subtree (R7d).
4. **Is it durable agent-facing truth?** A clearly-named file in the agent
   tree (R2). This is the default answer, and it is the right answer far more
   often than the others.
5. **Is it a friendly orientation for a human?** The human tree — as a
   summary plus a link into the agent tree, never as a second copy of the
   depth (R3).
6. **Is it a captured upstream document?** The remote tree, with provenance
   (R2b). Never rewritten in place.
7. **Is it none of these?** Then it is probably not durable, and it belongs
   in the commit message, a plan journal entry, or nowhere.

Then, on every OTHER surface that needs the reader to know it: a pointer
(R4), or a tracked quote (R4b) if inline text genuinely serves better.

### What a compliant pointer looks like

Non-compliant — a rules file that has quietly become a second canonical home:

```markdown
# Database migrations

Run migrations with:

| Environment | Command                   |
| ----------- | ------------------------- |
| local       | `make migrate`            |
| staging     | `make migrate ENV=stage`  |

1. Take a backup first.
2. Run the command above.
3. Verify with `make migrate-status`.
```

It carries a table, a fenced command and a numbered procedure — three
forbidden content classes (R4). When the staging command changes, this file
becomes confidently wrong and will be read far more often than the doc that
was correctly updated.

Compliant — same surface, same reader, no duplication:

```markdown
# Database migrations

You are about to run or edit a database migration.

Always take a backup before migrating, and never migrate staging from a
local checkout.

See [CLAUDE/Migrations.md](../../CLAUDE/Migrations.md) for the commands,
the environment matrix and the verification step.
```

Note what survived: the trigger statement, the rule in two imperative lines,
and the link. Note what the pointer legitimately ADDS — "never migrate
staging from a local checkout" is an application note for THIS surface's
reader (R4's clause (b)), not a restatement of the target.

### Retiring a duplicate

The order is not negotiable, because getting it wrong deletes the only copy:

1. **Find the canonical home.** If there is not one, create it — this is the
   step people skip, and it is the step R7a exists to insist on.
2. **Move the content there**, in full, in the register that tree uses.
   Reconcile any differences between the copies deliberately; do not assume
   the one you are keeping is the newer one.
3. **Verify it is really there** — read the destination file, do not trust
   the diff.
4. **Only then thin the duplicate** to a pointer.
5. **Commit the promotion and the thinning together**, so the corpus is never
   in a state where the content exists nowhere.

A shrinking satellite with no corresponding growth in the canonical tree is
exactly the shape of a botched promotion, and the commit gate says so.

---

## Enforcement

### The deterministic checks

Each check enforces one narrow, mechanical property. The table is the map
from rule to instrument:

| Check id                     | Rule    | Stages              | Can block?                                            |
| ---------------------------- | ------- | ------------------- | ----------------------------------------------------- |
| `pointer-resolves`           | R6      | edit, staged, sweep | Yes — only for a link NEW in this edit                |
| `at-import-census`           | R6      | edit, sweep         | Yes — only for an import NEW in this edit             |
| `duplicate-block`            | R1, R4  | edit, sweep         | **Never** — advisory by construction                  |
| `quote-drift`                | R4b     | edit, sweep         | Yes at edit — any drifted quote in the edited file    |
| `quote-source-stale`         | R4b     | edit                | **Never** — advises which quoters to re-check         |
| `rules-file-shape`           | R7a     | edit, sweep         | Yes — worse-only (a metric grew, or a new bad file)   |
| `rules-file-orphan-shrink`   | R7a     | staged              | **Never** — a prompt to check, not a verdict          |
| `module-doc-budget`          | R7d     | edit, sweep         | Yes — registered docs over the block tier, worse-only |
| `generated-doc-hand-edit`    | R10     | edit, sweep         | Yes at edit — the path matches a manifest glob        |
| `plan-promotion-disposition` | R8      | staged              | **Never** — weak keyword approximation                |
| `source-tree-markdown`       | R2, R7d | sweep               | **Never** — always advisory                           |

Two properties of that table are worth internalising:

- **Sweep findings are always advisory**, on every check. A sweep has no
  before-and-after, so it has nothing to make a worse-only judgement with.
- **Several checks can never block at all**, by construction rather than by
  configuration. Setting `block` for one of them changes nothing — mode can
  only escalate a finding the check itself marked block-eligible. A check
  that never marks one has no blocking path to unlock.

### The three surfaces and the CLI

| Surface         | When it runs           | What it judges                              |
| --------------- | ---------------------- | ------------------------------------------- |
| **Edit**        | at `Write`/`Edit` time | the would-be content of one file            |
| **Commit gate** | at `git commit`        | the staged tree, including cross-file drift |
| **Sweep**       | at session start       | the whole corpus; reports, never blocks     |

Run them by hand at any time:

```bash
.claude/hooks-daemon/bin/hooks-daemon docs-qa --sweep           # whole corpus; exit 1 on drift (CI-able)
.claude/hooks-daemon/bin/hooks-daemon docs-qa --check-staged    # staged-tree commit-gate checks
.claude/hooks-daemon/bin/hooks-daemon docs-qa --lint <FILE>     # edit-stage checks against one file
.claude/hooks-daemon/bin/hooks-daemon docs-qa --sweep --json    # machine-readable findings
```

The CLI runs **regardless of `documentation.enabled`** — an explicit
invocation is consent. The `enabled` switch governs the HANDLERS, which act
without being asked.

For the semantic half, dispatch the `hooks-daemon-docs-qa` agent (the
`docs-qa` skill is a thin shim that does exactly this). It ships opt-in; if
the dispatch fails because the agent is not deployed, enable it in
`.claude/hooks-daemon.yaml` and restart the daemon. It also consumes a
comment-block finder for its R7e hunt:

```bash
.claude/hooks-daemon/bin/hooks-daemon find-comment-blocks <source-root> --json
```

Both are **finders**: they list candidates, they never judge content. The
judgement is the agent's, or the reviewing human's.

### Configuration reference

| Key                                       | Default                     | Governs                                                 |
| ----------------------------------------- | --------------------------- | ------------------------------------------------------- |
| `documentation.enabled`                   | `false`                     | Master switch for the HANDLERS; the CLI runs regardless |
| `documentation.trees.agent`               | `CLAUDE`                    | Agent tree root (R2)                                    |
| `documentation.trees.human`               | `docs`                      | Human tree root (R3)                                    |
| `documentation.trees.remote`              | `remote-docs`               | Vendored upstream tree root (R2b)                       |
| `documentation.qa.edit_mode`              | `warn`                      | Edit-stage mode: `warn` or `block`                      |
| `documentation.qa.commit_gate_mode`       | `warn`                      | Commit-gate mode: `warn` or `block`                     |
| `documentation.qa.sweep_mode`             | `advise`                    | Sweep mode: `advise` or `off` — never blocks            |
| `documentation.qa.check_modes`            | `{}`                        | Per-check override, keyed by check id                   |
| `documentation.qa.grandfather_allowlist`  | `[]`                        | File globs held to advise-only forever (R12)            |
| `documentation.qa.generated_docs`         | daemon artefacts pre-seeded | Manifest of generated docs: `glob` + `generator` (R10)  |
| `documentation.qa.registered_module_docs` | `[]`                        | Sub-`CLAUDE.md` files that ARE a canonical home (R7d)   |
| `documentation.qa.resident_at_imports`    | `["CLAUDE.md"]`             | The `@`-import allowlist (R6)                           |
| `documentation.qa.scope_exclude_globs`    | `[]`                        | Files removed from the corpus entirely — frozen records |

**`grandfather_allowlist` and `scope_exclude_globs` are not synonyms**, and
choosing the wrong one is a common mistake:

- **Grandfathered** — the file is still INDEXED. Its links are still checked,
  its blocks still participate in duplicate detection, other files' pointers
  into it still resolve. Only the SEVERITY is capped at advise. Use it for a
  file that is non-compliant but live.
- **Scope-excluded** — the file is INVISIBLE. It is not in the corpus at all,
  so nothing resolves against it and nothing it contains is compared with
  anything. Use it only for a genuinely frozen historical record — a
  versioned upgrade guide, a self-labelled archived draft — whose links and
  structured blocks should never be re-verified against current truth.

Excluding a live document because it is noisy hides real drift and breaks
pointer resolution for every file that links to it.

### Adopting the ruleset in an existing corpus

The ratchet is per-check and per-project, and the order that works is:

1. Turn on the **sweep** first (`documentation.enabled: true`, everything at
   its advisory default) and read the report. Do not fix anything yet.
2. **Triage the report by hand.** Findings on files you are about to rewrite
   anyway need no action; findings on frozen records go to
   `scope_exclude_globs`; the long tail of standing violations goes to
   `grandfather_allowlist`.
3. **Ratchet one check at a time** to `block` via `check_modes`, starting
   with the ones whose findings you have driven to zero. Worse-only semantics
   (R12) mean this cannot block work on files nobody is making worse.
4. **Remove grandfather entries as you fix them.** An allowlist that only
   grows is a permanent exemption wearing a temporary label.

Never adopt by switching `edit_mode` to `block` globally on day one. Every
project's corpus is non-compliant before the rules arrive; a day-one global
block fires on unrelated work, and the rational response is to disable the
whole subsystem.

---

## What is NOT a violation

The rules are strict, and it is worth being explicit about what they do not
forbid — over-application is its own failure mode.

- **A generated doc that duplicates code.** That is generation. Declare it
  (R10).
- **A vendored upstream document that overlaps your own docs.** That is a
  capture, not a copy of your fact (R2b).
- **A human-tree page that summarises an agent-tree page.** That is the
  intended shape. What R3 forbids is restating the DEPTH.
- **Orientation prose in a pointer.** Up to roughly three sentences saying
  what the target covers and why this reader would follow it is explicitly
  permitted by R4's clause (a). A pointer is judged on content class, not on
  length.
- **A surface-specific application note.** "In this module, that rule means
  X" is not in the target and is exactly what R4's clause (b) allows.
- **A verbatim excerpt wrapped in `ssot-quote` markers.** Tracked repetition
  is a supported mechanism, not a tolerated one (R4b).
- **A comment explaining why THIS code is shaped this way.** That is
  current-state rationale and belongs in the code (R7e). What migrates out is
  knowledge a reader outside the file would need.
- **A canonical doc citing a plan as history.** Provenance is encouraged;
  only DELEGATION to the plan is forbidden (R9).
- **Two documents covering the same SUBJECT at different depths for
  different audiences.** That is the model working. Two documents asserting
  the same FACT is the violation.

---

## The one-line version

Every durable fact has exactly one home, in the agent tree by default.
Everywhere else points at it, and the only copies allowed are the ones that
know they are copies.
