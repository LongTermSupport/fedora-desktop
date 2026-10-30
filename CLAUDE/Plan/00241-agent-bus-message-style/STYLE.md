# Agent bus message style

**Version 1.** A change to this guide raises the version and says what changed, so a message
can be read against the version it was written to.

There are two styles, one for each kind of reader.

- **Messages a person reads** (sections 1 to 5): reports (`pingbus report`), questions
  (`pingbus ask-human`) and the posts in their threads. Agents write markdown, and pingbus
  sends it as the plain `body` (the markdown itself, readable as it stands) plus the HTML
  Element draws (`format: org.matrix.custom.html`, `formatted_body`).
- **Messages between agents** (section 6): `pingbus say` to another member. Plain text in
  a terse line protocol that costs few tokens to read. A person can still follow it, but
  that is secondary.

## 1. Every message starts with a headline

```
#### <emoji> <Kind> · <what happened, in under ten words>
```

The headline alone must tell the reader whether to read on. It names one kind, from this
list:

| Kind     | Emoji | Use it for                                                                    |
| -------- | ----- | ----------------------------------------------------------------------------- |
| Info     | ℹ️    | Something worth knowing. Nothing to do.                                       |
| Update   | 🔄    | Progress on work in flight: what moved, what is next.                         |
| Confirm  | ✅    | Done, verified or agreed, with the evidence.                                  |
| Request  | 📨    | Asking a named member to do something.                                        |
| Question | ❓    | Needs an answer, from whom, and what each answer would change.                |
| Answer   | 💬    | A reply to a question, which it quotes.                                       |
| Warning  | ⚠️    | Might need attention. Work goes on.                                           |
| Error    | ❌    | Something failed, with its cause. Says who is handling it.                    |
| Critical | 🚨    | Work stopped, or data, security or a team is at risk. A person is needed now. |

Severity follows the kinds downwards. Use the lowest one that is true: a warning is not an
error, and an error someone is already fixing is not critical.

## 2. The body: one fact per line

- One bullet per fact, with its subject in bold first: `- **00228:** the VM test passed.`
- A blank line between groups. No paragraph longer than three lines.
- Status inside a list uses these markers: ✅ done, ⏳ running or waiting, ⛔ blocked,
  🙋 needs you, ➖ skipped.
- A `> blockquote` holds what someone else said: a question being answered, an error
  message, a log line.
- `code` for a command, a path, a commit or a plan task. Use a code block only for a
  command to copy or several lines of output.
- `**bold**` for the subject of a line and the one thing that must not be missed. Use
  `*italics*` sparingly, for a term being defined.
- No tables. They wrap badly on a phone, and the plain `body` shows them as pipes.

## 3. What needs a person comes last

```
**🙋 Needs you:**
- **A6** (optional): CPU-only torch on machines with no NVIDIA GPU? Say "A6 a" or "A6 wait".
```

Each item can be answered aloud, so the answer can be spoken through dictation.
Something only a person can do, at the screen, says what to do and roughly how long it
takes.

## 4. Length

Fifteen lines at most. Anything longer goes in a file, and the message names its path
and gives its three main points.

**Draft a message in a `.txt` file, never `.md`.** The repository's markdown formatter
rewrites a `.md` file when it is written, before pingbus reads it: grouped bullets become
one loose list with a blank line between every item, so Element shows them spaced apart and
the grouping is lost.

## 5. An example

```
#### 🔄 Update · 00228 VM test running again

- ✅ **00191:** the DBF rule blocks; 111 places fixed; review passed
- ✅ **00192:** vmtest clears a cut run's leftover domain
- ⏳ **00228:** consumer VM test, now without pip's cache (about 1.5 h)

**🙋 Needs you:**
- **A7:** 8 bash places with the same bug. Port them to Go, or fix them in bash? Say "A7 a" or "A7 b".
```

## 6. Between agents: the line protocol

Plain text, no markdown and no emoji. Every word costs the reader tokens, so every line
carries a fact.

- **The first line** is `<KIND> <subject>: <claim>`. KIND is one of `INFO`, `UPD`, `DONE`,
  `REQ`, `Q`, `A`, `WARN`, `ERR` or `CRIT`, the nine kinds of section 1. The subject is a
  plan (`00228`), a plan task (`00228/6.7`) or a component (`vmtest`).
- **Each further line** is one fact, `key: value`. Use the keys that fit: `do`, `report`,
  `cause`, `result`, `next`, `kept`, `ref`, `note`.
- **References are exact:** a short commit (`50f6019ee`), a path from the repository root,
  a scenario or play by name. A reply uses `pingbus say --re`, so it does not restate the
  message it answers.
- **Results are counts:** `20/20`, `rc=1`, `15.1G`, `~1.5h`.
- **Leave out** greetings, thanks, apologies, restating the request, hedging and
  narration of what you are about to do.
- **Eight lines at most.** Anything longer goes in a file under `untracked/`, named in a
  `ref:` line.
- **A request** says what to do (`do:`) and what to send back (`report:`). **An answer**
  gives its verdict on the first line.

```
REQ 00228: run meta-deploy at 50f6019ee
do: ./CLAUDE/Plan/meta-deploy.bash (00228 acceptance ~1.5h, then 10 plays)
report: per scenario n/N; per failed play the task and the error

ERR 00228/6.7: desktop-consumer-install 15/20
cause: pip RealtimeSTT ENOSPC, log line 2621
kept: the run's overlay, 15.1G
next: measure it read-only? (say yes)

DONE 00228/6.7: acceptance 3/3 scenarios
result: consumer 20/20, fresh 16/16, server-fast 13/13
```
