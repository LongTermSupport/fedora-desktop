# Hooks Daemon Investigation Report

Generate a comprehensive investigation report for a hooks daemon issue. This is an LLM-driven analysis — collect evidence, build a timeline, and write a narrative report.

## Problem Description

The user reported: **$ARGUMENTS**

## Instructions

Follow these steps exactly. Do not skip any step.

### Step 1: Ensure output directory exists

```bash
# Create untracked directory if it doesn't exist
mkdir -p ./untracked

# Ensure .gitignore exists in untracked/ so reports are never committed
if [ ! -f ./untracked/.gitignore ]; then
    echo "*" > ./untracked/.gitignore
fi
```

### Step 2: Collect evidence

Gather ALL of the following. Save raw output to shell variables or temp files for use in the report.

#### 2a. Daemon status and health

```bash
$PYTHON -m claude_code_hooks_daemon.daemon.cli status 2>&1
```

#### 2b. Daemon logs (last 200 lines)

```bash
$PYTHON -m claude_code_hooks_daemon.daemon.cli logs 2>&1
```

#### 2c. Loaded handlers

```bash
$PYTHON -m claude_code_hooks_daemon.daemon.cli handlers 2>&1
```

#### 2d. Configuration file

Read `.claude/hooks-daemon.yaml` — include the full contents in the report.

#### 2e. Recent transcript data

Look for transcript archives in `untracked/transcripts/` or `.claude/transcripts/`. Read the most recent 1-3 files if they exist. These contain conversation history that may reveal what Claude was doing when the issue occurred.

#### 2f. Git context

```bash
git log --oneline -20
git status
```

#### 2g. Environment

```bash
echo "Hostname: $HOSTNAME"
echo "Python: $($PYTHON --version 2>&1)"
echo "OS: $(uname -a)"
echo "Container: ${YOLO_CONTAINER:-not detected}"
```

#### 2h. Hook script check

Verify hook scripts are present and executable:

```bash
ls -la .claude/hooks/
```

#### 2i. Recent daemon errors

Search daemon logs for ERROR, WARNING, traceback, and exception patterns. Note timestamps.

### Step 3: Build a timeline

From the evidence collected, construct a chronological timeline:

1. Parse daemon log timestamps to establish when events occurred
2. Cross-reference with transcript data to see what Claude was doing
3. Cross-reference with git log to see what commits were made
4. Identify the sequence: what happened before, during, and after the issue
5. Note any gaps (daemon restarts, missing log entries, etc.)

### Step 4: Write the report

Write a single markdown file with this structure:

```markdown
# Hooks Daemon Issue Report

**Date**: {current date and time}
**Reporter**: Claude Code (automated investigation)
**Problem**: {one-line summary from user description}

---

## Executive Summary

{2-3 paragraphs explaining what happened, what went wrong, and the likely root cause. Written for a maintainer who has no context.}

## Timeline

| Time | Event | Source |
|------|-------|--------|
| ... | ... | daemon log / transcript / git |

{Chronological table of events leading up to, during, and after the issue.}

## Evidence

### Daemon Status

{Output from status command}

### Daemon Logs

{Relevant log excerpts — highlight errors and warnings. Include last 50 lines minimum.}

### Configuration

{Full hooks-daemon.yaml contents in a code block}

### Loaded Handlers

{Handler list output}

### Transcript Excerpts

{Relevant transcript sections showing what Claude was doing when the issue occurred. If no transcripts available, note that.}

### Environment

{System info}

### Hook Scripts

{ls -la output}

## Analysis

{Detailed analysis of what went wrong:
- What was the immediate cause?
- What was the root cause?
- Were there contributing factors?
- Has this happened before? (check git log for similar fixes)}

## Suggested Fix

{If a fix is apparent from the evidence, describe it. Otherwise, describe what additional information a maintainer would need.}

## Raw Evidence

<details>
<summary>Full daemon logs (last 200 lines)</summary>

{Complete log output in code block}

</details>

<details>
<summary>Full git log (last 20 commits)</summary>

{Git log output in code block}

</details>
```

### Step 5: Save the report

Generate a filename slug from the problem description:

- Lowercase
- Replace spaces with hyphens
- Remove special characters
- Truncate to 50 characters
- Prepend with `hooks-daemon-`

Write the file to: `./untracked/hooks-daemon-{slug}.md`

### Step 6: Give the user instructions

After writing the report, tell the user:

```
Report saved to: ./untracked/hooks-daemon-{slug}.md

To share this report with the hooks daemon maintainers:

1. Open a GitHub issue at: https://github.com/anthropics/claude-code-hooks-daemon/issues
2. Title: "Bug Report: {problem summary}"
3. Paste the contents of the report file, or attach it
4. Add any additional context from your own observations

Alternatively, share the file directly with whoever maintains the hooks daemon in your organisation.
```
