# Plan 00092 — Security model for child-claude spawn mode

This document exists because "without any security degradation" is the binding
requirement of Plan 00092, and a requirement that is not testable is not a
requirement. It states what is actually being defended, names the seven invariants
that define "no degradation" here, and records the one alternative that would give
a real boundary and why it is out of scope.

---

## 1. Start with the uncomfortable fact

**Inside a CCY container the agent is root, and the OAuth token is already
reachable.** It sits in PID 1's environment because `claude-yolo:3021` passes
`-e CLAUDE_CODE_OAUTH_TOKEN` to `podman run`, and any root process can read
`/proc/1/environ`. This was confirmed by measurement, not assumed.

Claude Code's removal of `CLAUDE_CODE_OAUTH_TOKEN` from the Bash-tool environment
is therefore **not a security boundary in this container**. It is an
accident-prevention measure: it stops the credential turning up in routine command
output, in a transcript, in a log, or in a `git commit` of a debug file, none of
which require anyone to be acting badly.

Two consequences follow, and the whole design rests on them.

1. A feature that lets a child process reach a token the parent could already
   reach **removes no boundary**, because there was none to remove.
2. A feature that makes the token *easier to touch by accident* **is** a
   degradation, even though it removes no boundary. That is the thing to avoid.

Any claim that an in-container flag "controls" access to the token would be false.
The flag controls whether the tooling and the guidance are installed. It does not
and cannot restrain an agent running as root. Plan 00092 says so out loud rather
than implying otherwise.

---

## 2. What is actually being defended

Ranked by how likely it is to happen.

| #   | Threat                                                                                                                               | Realistic?                                                                | Addressed by        |
| --- | ------------------------------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------- | ------------------- |
| T1  | The token is copied into a transcript, a log, a report, or a commit, and this is a **public repository**                             | High. This is the failure this project already writes rules about.        | I1, I2, I3, I4      |
| T2  | The token is written under `/workspace`, which is a host mount, so it outlives the ephemeral container on the user's real filesystem | High, and silent                                                          | I1                  |
| T3  | The capability is enabled once, then persists into later sessions that did not ask for it, because `/root/.claude` is host-persisted | High. Measured: `/root/.claude` is a symlink to `/workspace/.claude/ccy`. | I6                  |
| T4  | A child runs with more authority than the parent, so "spawn a child" becomes a privilege-escalation step                             | Moderate, if the wrapper is convenient rather than careful                | I5                  |
| T5  | Recursive spawning exhausts the subscription quota or the container                                                                  | Moderate                                                                  | I7                  |
| T6  | The flag is set but the image predates the feature, so the session runs in a state nobody designed                                   | Moderate, on version drift                                                | Fail-fast, Task 4.4 |

**Explicitly not defended against:** the agent deliberately reading
`/proc/1/environ` itself. That is possible today, before this plan, and no
in-container mechanism can prevent it while the agent is root. Pretending
otherwise would be the one genuinely dangerous thing this document could do.

---

## 3. The seven invariants

Each is a property of the finished feature, and each has a probe in
`acceptance.bash`. A probe that cannot fail is not a probe; each one is written so
that it goes red when the invariant is broken, and that is verified by breaking it
deliberately at least once during development.

**I1 — No on-disk copy.** The token is written to no file anywhere in the
container, and in particular to nothing under `/workspace`. There is no token file,
no cache, no dotenv, no settings key.

**I2 — Never in argv.** The token never appears in any process's command line, so
never in `/proc/*/cmdline`. It is passed to the child through the environment, the
same way CCY already passes it to `podman run` by name.

**I3 — Never inheritable by the agent's shell.** No new environment variable
carrying the token is visible in the Bash-tool environment. The wrapper's own
process gets it; the caller's shell does not. Confirmed on the prototype: after a
successful child run the calling shell still had no token.

**I4 — Never on a stream.** The token reaches neither stdout nor stderr, so it
cannot land in a transcript, a captured log, or a plan `logs/` file. Diagnostics
name the variable, never its value.

**I5 — Equal authority, never greater.** The child runs as the same user, in the
same container, the same network namespace and the same mounts as the parent. The
wrapper adds no arguments of its own. It does not inject
`--dangerously-skip-permissions`, a model, a settings file, or a permission mode.
Whatever the caller passes is what the child gets.

**I6 — Off means off, including yesterday's on.** With the flag absent, a session
has no wrapper on `PATH` and no child-claude skill in `/root/.claude/skills/`, even
when the immediately preceding session had the flag set. This requires active
removal, because that directory is host-persisted and gitignored. Without I6 the
mode cannot be switched off, only switched on.

**I7 — Bounded depth, as an accident guard.** A child launched through the wrapper
does not spawn an unbounded tree by mistake. `CCY_CHILD_CLAUDE_DEPTH` is incremented
by the wrapper and checked against `CCY_CHILD_CLAUDE_MAX_DEPTH`, default 1. The counter
is an ordinary variable and survives into the child's own Bash-tool environment,
since only the credential name is scrubbed, so an honest caller is counted correctly.

It is an accident guard in exactly the sense section 1 uses for the flag itself, and
this document would be lying if it said otherwise: a child can run
`CCY_CHILD_CLAUDE_DEPTH=0 ccy-claude`, or skip the wrapper and read
`/proc/1/environ` directly. What I7 stops is the runaway loop nobody intended — the
recursive fan-out that spends a subscription before anyone notices — not a caller
who has decided to nest.

---

## 4. Why the two easier designs were rejected

Both work. Both were tried or checked against the documentation. Both degrade T1.

**The `env` key in `settings.json`.** Documented to set variables for the session
and its subprocesses, so it would solve the problem in one line. It is disqualified
by T2 alone: `/root/.claude` resolves to `/workspace/.claude/ccy`, so this writes a
live credential into the user's real project directory on the host, in plaintext,
where Anthropic documents that file permissions are the only protection.

**A second environment variable under a name Claude Code does not scrub.** Only the
exact name `CLAUDE_CODE_OAUTH_TOKEN` is stripped; every other variable CCY passes
survives into the Bash tool, so an alias would be inherited automatically and need
no wrapper at all. It is disqualified by T1: the credential would then be present
in every command's environment for the whole session, one `env` away from any
output the agent produces. That is exactly the accident the scrub prevents, and
re-creating it repo-wide to save a fifteen-line script is a bad trade.

Reading `/proc/1/environ` inside the wrapper keeps the value in one short-lived
process that the agent does not compose the arguments of, which is why it wins.

---

## 5. The alternative that would give a real boundary

Run `claude` in CCY as a **non-root user**, with the token readable only by a
separate uid reached through a narrow, audited helper. Then the scrub would be a
boundary, the wrapper would be an authorisation point rather than a convenience,
and I5 could be enforced rather than merely honoured.

It is out of scope for Plan 00092 and should not be smuggled in. CCY runs as root
deliberately — under rootless Podman, container UID 0 maps to the host user, which
is what makes the `/workspace` mount work at all. Changing that touches the image,
the entrypoint, the mount strategy, every tool in the container, and the SSH and
`gh` setup. It is its own plan. Recording it here means the next reader knows the
current design's ceiling was chosen, not overlooked.

---

## 6. What a reviewer should check

- Does any probe in `acceptance.bash` pass when the thing it names is broken?
  Break each one once and watch it go red.
- Does any diagnostic anywhere print `$CLAUDE_CODE_OAUTH_TOKEN` rather than the
  name of the variable?
- Does the disabled path actually delete, or only decline to install?
- Does the wrapper add any argument the caller did not pass?
- Does any document in this plan claim the flag restricts the agent? It must not.
