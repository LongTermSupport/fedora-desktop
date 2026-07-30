# Plan Triage — how to establish facts

Triage is how a plan replaces guesses with grounded facts. This document is
the canonical reference for writing and running plan-local triage scripts.

It exists because the underlying rule — already stated in
[AgentNotes.md](AgentNotes.md#never-assume-or-hallucinate--decide-only-from-grounded-triaged-facts)
— was being followed in spirit and then abandoned in practice: a `triage.bash`
would be written, and then the next three diagnostic questions would be
answered by pasting ad-hoc commands into chat instead of extending it.

---

## The rule (in stone)

> **Every diagnostic probe goes into the plan's `triage.bash`. Never hand the
> user a one-off command to run in chat.**

If you need a data point you do not have, the response is *always* "add a probe
and have them re-run the script" — never "run this command and paste the
output".

### Why ad-hoc commands are the wrong answer

- **They are not reproducible.** The next run, next session, or next person
  gets a different set of commands and a different answer.
- **The output goes to a terminal, not a file.** It then has to be copy-pasted
  back, which is lossy, truncated, and tedious for the user.
- **An agent cannot read a terminal.** `triage.bash` writes into
  `untracked/reports/`, which is gitignored *and* bind-mounted into the CCY
  container — so the agent reads the report at the same repo-relative path,
  with no copy-paste at all.
- **They accumulate silently.** Five commands in chat is knowledge that
  evaporates when the session ends. Five probes in a script is a diagnostic
  the plan owns.
- **They put interpretation on the user.** A probe in the script can carry the
  `READ THIS FOR:` line explaining what the output means.

The only legitimate ad-hoc command is one that *creates or modifies* the
triage script itself.

---

## Triage vs acceptance — different jobs

| Script            | Job                                     | Renders a verdict? |
| ----------------- | --------------------------------------- | ------------------ |
| `triage.bash`     | Gather facts. Report what *is*.         | **No**             |
| `acceptance.bash` | Confirm the fix worked. Pass/fail gate. | **Yes**            |
| `deploy.bash`     | Run the plan's Ansible. HOST-only.      | n/a                |

A verdict ("the store loads ✅", "H1 confirmed") belongs in the
acceptance/verify gate, **not** in triage. Triage may say *"successful uploads
per filename: 9"* and even explain how to read it; it must not say *"therefore
the camera is broken"*.

---

## Anatomy of a triage script

### It is read-only

Triage changes nothing: no service started or stopped, no file moved or
deleted, no firewall or config touched. It must be safe to run repeatedly, on
a live system, mid-incident. Say so in the header comment.

Packet captures, `find`, `stat`, `systemctl status`, `journalctl` are all
fine. Anything with a side effect is not triage.

### It writes its own report

```bash
REPO_ROOT="$(git rev-parse --show-toplevel)"   # NOT a fixed ../ hop — the
                                               # script must survive the move
                                               # into Completed/
REPORTS_DIR="$REPO_ROOT/untracked/reports"
mkdir -p "$REPORTS_DIR"
LOG="$REPORTS_DIR/<plan-topic>-triage.log"
exec > >(tee "$LOG") 2>&1
```

Fixed filename, so the latest run is always at a predictable path. `tee` to a
file — never to `/dev/null`.

### The `probe()` helper

A non-zero exit status is **data**, not a failure. Capture it and carry on:

```bash
probe() {
    local label="$1"; shift
    local out rc
    if out="$("$@" 2>&1)"; then rc=0; else rc=$?; fi
    printf '### %s  (rc=%d)\n%s\n\n' "$label" "$rc" "${out:-(no output)}"
    return 0
}

probe "disk usage" df -h /srv/thing
```

This is the sanctioned way to suppress noise without hiding errors. Do **not**
use `2>/dev/null` or `|| true` — both are error-hiding patterns the hooks
daemon blocks, and both discard the reason, which is often the finding.

### Probe helpers are functions, not `bash -c` strings

`probe "label" bash -c 'for x in $(...); do ...; done'` defeats shellcheck and
tangles quoting. Define a function and pass its name — `probe` runs `"$@"` in
the same shell, so functions work:

```bash
show_wifi_links() {
    local dev
    for dev in $(iw dev | awk '/Interface/ {print $2}'); do
        echo "== $dev"; iw dev "$dev" link
    done
}
probe "wifi link quality" show_wifi_links
```

### Tell the reader what matters

The person reading a 400-line report needs a pointer to the decisive section:

```bash
echo "### successful uploads per filename  (count | filename)"
echo "###   1 per frame = healthy;  >1 = the camera is re-sending"
```

Finish with a closing banner naming the section to read first.

### `--help` must work before anything else

Parse arguments **before** resolving environment (users, paths, services).
A script that dies resolving a missing user before it can print its own help
is useless on exactly the machine that needs diagnosing.

Beware `set -euo pipefail` with probe-style assignments:

```bash
# WRONG — getent exits 2 when the key is absent; under `set -e` + pipefail
# this kills the script at this line, printing nothing, so the friendly
# error below is unreachable.
UPLOAD_DIR="$(getent passwd camera | cut -d: -f6)"
if [ -z "$UPLOAD_DIR" ]; then echo "ERROR: ..."; exit 1; fi

# RIGHT — probe, then check.
UPLOAD_DIR=""
if _line="$(getent passwd camera)"; then
    UPLOAD_DIR="$(printf '%s' "$_line" | cut -d: -f6)"
fi
if [ -z "$UPLOAD_DIR" ]; then echo "ERROR: ..." >&2; exit 1; fi
```

### Never write a misleading empty result

If a probe cannot produce meaningful data, **fail** rather than emit an empty
section. An empty capture reads as evidence of absence, which is worse than no
capture:

```bash
if ! systemctl is-active --quiet vsftpd; then
    echo "ERROR: vsftpd is not running — no traffic to capture." >&2
    echo "  Start a session in another terminal, then re-run with --capture." >&2
    return 1
fi
```

### Secrets never reach the report

The report is a file. Filter credentials *before* anything is written, not
after — a trace of a plaintext protocol will contain the password otherwise.

### A missing tool is an IaC gap

If triage needs a tool the host lacks, **do not** tell the user to install it
and **do not** make the script skip it. Declare it in the playbook that owns
the feature and point at the play:

```bash
if ! command -v tcpdump > /dev/null; then
    echo "ERROR: tcpdump is not installed." >&2
    echo "  It is declared in play-<feature>.yml. Deploy it with:" >&2
    echo "    ansible-playbook playbooks/imports/.../play-<feature>.yml" >&2
    echo "  Do NOT install it by hand." >&2
    return 1
fi
```

See CLAUDE.md, "Missing Dependencies — Fail Fast, Fix in IaC".

### Active probes get a flag

Passive reporting is the default. Anything that takes time or needs a live
session (a packet capture, a timed sample) goes behind an explicit flag so the
common case stays instant:

```
triage.bash              # passive report
triage.bash --capture    # additionally record a live capture, then report
```

---

## Separating fact from hypothesis

Triage output is fact. The plan document must keep facts and hypotheses in
**different buckets**, and never let a hypothesis harden into an asserted
cause.

- Number the facts (`F1`, `F2`, …) with a **source column** naming the
  evidence for each.
- Number the hypotheses (`H1`, `H2`, …) and state, for each, what observation
  would confirm or refute it.
- Record **unverified premises** the analysis leans on. These are the
  dangerous ones — assumptions invisible enough to be mistaken for facts.
- When a fact settles a premise or kills a hypothesis, say so explicitly
  rather than quietly editing the old text.

A fact about one moment does not license a claim about every moment. "No
profile exists *now*" is not "no profile has *ever* existed".

---

## Checklist

- [ ] Every diagnostic question answered by a probe in `triage.bash`, not a
  chat command
- [ ] Read-only; safe to re-run on a live system
- [ ] Writes its report to `untracked/reports/<topic>-triage.log`
- [ ] `git rev-parse --show-toplevel` for the repo root, not `../`
- [ ] `probe()` used; no `2>/dev/null`, no `|| true`
- [ ] Probe helpers are functions, not `bash -c` strings
- [ ] `--help` works before any environment resolution
- [ ] Fails rather than emitting a misleading empty section
- [ ] Secrets filtered before anything is written
- [ ] Missing tools declared in a playbook, never installed by hand
- [ ] Decisive section flagged with a `READ THIS FOR:` pointer
- [ ] Passes `./scripts/qa-all.bash` (plan-folder scripts are linted too)

---

## See also

- [PlanWorkflow.md](PlanWorkflow.md) — plan structure; "Plan-Local Scripts &
  Artifacts" (the transient-vs-persistent test)
- [AgentNotes.md](AgentNotes.md) — the originating rule, and the Plan 00062
  incident that produced it
- [StderrHygiene.md](StderrHygiene.md) — a report command's stdout **is** its
  payload; that is why triage prints rather than returning values
