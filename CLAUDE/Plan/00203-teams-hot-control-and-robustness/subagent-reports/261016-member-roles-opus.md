# Task 3.8: every member kind has the same roles (`fix/teams-member-roles`)

The owner: on an LXC member's row of `ccy-teams`, Enter offered Park first and highlighted, and
no role. Then: "any member that gets added by any mechanism should be fully equivalent in terms
of roles". Built test-first; red runs are in the JOURNAL entry of 2026-10-16.

## One role-change path, by kind only at the last step

`src-go/internal/teams/roles` is the one place a role changes. Its callers ask their own
question first: `seats.ChangeRole` (every picker member row, settings, the launch chooser),
hot (`session role`, `add --role`, a remove's hand-to) and `lxc-add`'s takeover (its
demote-then-write with rollback now goes through it). Each step:

- a member that has a handle is set live, `sudo agent-bus set-role` (so the team record,
  which routes messages, changes at once), for every kind;
- a member declared by an `agent_bus_lxc_members` entry also has its entry set first
  (`declare lxc-role`), then the team is applied through `ops.ApplyTeams`.
- Correction after Fable's review: as first committed (0be311bd) the entry and the live role
  were written BEFORE the play lock was waited for, which `ops.ApplyTeams` alone takes on
  `fix/teams-wait-play-lock`; a stopped wait would have said "nothing was changed" over a
  changed entry. Now (merged with that branch) `roles.Change` runs the entry, the live role and
  the apply in one `ops.Locked` hold, so a stopped or timed-out wait writes nothing; a change
  with no entry takes no lock.

Before this, every live path set a declared LXC member's role live only: the next apply put
the declared role back, or refused a second coordinator. Which entry a live LXC handle is was
not known anywhere; it is now read the way the play derives it: each candidate entry's
checkout probed as root (the lxc-add root reader), then a new `declare lxc-identity` gives its
repo and host (`lxc-members` now also prints each entry's checkout and seat). An entry whose
container is stopped stops the change before anything changes. A step that fails puts back the
steps before it.

## The picker

- Every member row, any kind, offers the role, worded in package `coordinator`:
  "Make <handle> the coordinator (<team> has none)", the takeover "Replace the coordinator,
  <holder> (...): it becomes a worker...", or "Make <handle> a worker (<team> then has no
  coordinator)". Each asks first, default No.
- LXC row order is exactly: role, Element, start a session, add an LXC agent, help, Park.
- Rule, tested over every row of every kind and every coordinator situation: no destructive
  action (park, remove, take down, shut down, demote) is first, and each comes after every
  other. Also applied to the Running sessions menu (Remove from a team last), its role
  question (a coordinator starts on "coordinator (now)"), settings (No coordinator last) and
  the take-down question (starts on "Leave it running").
- A test enumerates ccy here, ccy elsewhere, cc (host), LXC, docker and vm and asserts the same
  role line and the same shared actions in the same order.

## The CLI: `session role` takes a handle

`ccy-teams session role <session>|<handle> <team> worker|coordinator`. Chosen over a new
`member role` command: one command, no overlap; a session name is one way to name a member,
the handle the other. Same two-path question and `--demote-current`.

## How a role change reaches a running agent

Live for every kind, no restart. Routing (who is copied) changes at once. The agent is told:
ccy and cc sessions by the membership notice, as before; agent-bus-claude sessions (LXC,
host, docker, vm) by a new pingbus role notice (`told-roles.json`), from their watcher at once
or else at their next prompt. Before this, those learnt their role only at SessionStart, so a
running LXC agent's instructions kept the old role until it restarted.

Deploy note: the LXC kit reaches containers through play-agent-bus.yml. After it, the prompt
hook tells at once; a watcher already running keeps the old code until that session restarts
once.

## The status line shows the role

`👥 desktop (coordinator), other (worker)`. `pingbus status-line` reads each role on every
render through `inbox.cached_role`, from the same cached team record the role notice reads (a
test pins it equal to the hooks' survey), so a change shows at the next refresh with no restart.
It reads through `config.load_bundle`, imported only when there are teams to show, so loading the
module stays as cheap as before. Order: the team, its role, then its marks, `beta (worker) (down)`. A role that cannot be read is `(role?)`, with one stderr line naming the teams and why,
never a guessed "worker". A team and its role never wrap apart. The Plan 00201 M3 and Plan 00203
M3 acceptance checks now expect a real role for each team.

## Differences between kinds that remain

1. Attach and restore are on ccy seat rows only: only sessions on ccy's tmux can be attached
   from the host; an LXC agent is reached with lxc-attach.
2. A declared LXC member's role change also edits its host_vars entry and runs the apply
   (slower, takes the play lock), and needs its container running to tell which entry it is.
   Other kinds have no declared role.
3. `session role` waits for a ccy or cc session's report to show it was told; given a handle
   there is no report to wait on, so it says how the member learns it instead.
4. A takeover's question says "running" or "not running" for a ccy holder; for an LXC, host,
   docker or vm holder it says "whether it is running is not known here".
5. Two LXC entries of one team with the same repo and host and no seat set cannot be told apart
   from a handle: the change is refused, naming both, until each entry has its seat.
6. Same in both paths, so not a difference: a worker is not told the new coordinator's name
   when the coordinator changes (routing is right at once).

Not classed destructive (judgement): a person's "New password" (signs out their Element
sessions). It asks first, default No, and is never first in its menu.

## Versions and deploy

CCY 3.99.12.17, container 2.77.14 (pingbus and declare.py are baked into the image; .15/.12
before Fable's fixes, .16/.13 before the status-line role): renumber
at merge. Queued in `meta-deploy.list`: play-claude-yolo.yml, then play-agent-bus.yml.
