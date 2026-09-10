# Plan 00092 — Decisions

The reasoning behind the three choices that shaped this plan, extracted from `PLAN.md`
to keep that document lean. `PLAN.md` links here; nothing else should restate these.

The threat model these serve is [SECURITY-MODEL.md](SECURITY-MODEL.md).

---

## Decision 1: Recover the token from `/proc/1/environ`, not from a file or an env alias

**Context**: the child needs the token; three mechanisms could supply it.

**Options considered**:

- *`env` key in `settings.json`* — documented to reach subprocesses, but
  `/root/.claude` is the host-mounted project directory, so this writes a live
  credential into the project tree. Rejected outright.
- *A second, non-scrubbed variable name* — works, because only the exact name is
  stripped. It makes the credential readable by every command in the session and
  therefore by every transcript. A real widening. Rejected.
- *Read `/proc/1/environ` inside the wrapper* — the value never enters a variable
  the agent composes, never appears in argv, never reaches a transcript.

**Decision**: `/proc/1/environ`, because it is the only option that adds no new
exposure surface over what root in this container already has.

**Date**: 2026-09-02

---

## Decision 2: The opt-in lives in `ccy.env`, and is a capability declaration, not a security control

**Context**: an in-container gate cannot bind an agent running as root.

**Decision**: state this plainly rather than implying the flag contains anything.
`ccy.env` is already sourced as shell in-container, so the flag grants nothing the
file could not already do. Its job is to declare intent and to keep the tooling and
the skill out of sessions that did not ask for them.

**Date**: 2026-09-02

---

## Decision 3: Pass arguments through verbatim

**Context**: the wrapper could inject `--dangerously-skip-permissions` for convenience.

**Decision**: it does not. Injecting it would silently widen what a child may do,
which is precisely the degradation this plan forbids. The caller passes what it needs.

**Date**: 2026-09-02

---

## Decision 4: `--binary-files=text` in the I1 search, rather than a skipped-file count

**Context**: the I1 probe passed `--binary-files=without-match`, so `grep` never searched
any file it judged binary, while the pass line reported the whole enumeration as searched.
The review measured 31,914 of 84,222 files reported as searched but never opened.

**Options considered**:

- *Keep the skip and report it* — print `COVERAGE: n searched, k skipped as binary`. Honest,
  but it leaves the blind spot: a token in a `.cache` blob or a swap file is exactly threat
  T2, and the probe would report the leak as "skipped" rather than as a red.
- *`--binary-files=text`* — searches everything, so the enumeration count and the searched
  count are the same number.

**Decision**: `--binary-files=text`. It closes the blind spot and the reporting gap in one
change, and it removes an implementation dependence as a bonus: `grep` is ugrep in the CCY
container and GNU grep on the host, their default binary handling differs, and this is the
one mode measured to agree. `-l` means only file NAMES are ever printed, so no binary bytes
reach a log. Measured after the change: 85,054 of 85,054 searched, 0 skipped.

**Date**: 2026-09-10

---

## Decision 5: A name-colliding skill is reported and left in place, not removed

**Context**: I6 requires that a disabled session carry no child-claude skill, but
`/root/.claude/skills/` is the user's real host filesystem and a user-authored skill could
share the name.

**Decision**: `entrypoint.sh` removes the directory only when it is recognisably ours — the
shipped `SKILL.md` carrying `name: child-claude` in its frontmatter — and otherwise warns
and leaves it. The residue is inert, since a skill is guidance text and without the wrapper
on `PATH` there is nothing for it to invoke, so the cost of leaving it is a stale document
while the cost of removing it is someone else's work. `probe-invariant.bash I6` still reports
the directory's existence as a red, so the state is visible rather than silently tolerated.

Stated in full at [SECURITY-MODEL.md](SECURITY-MODEL.md) under I6.

**Date**: 2026-09-10
