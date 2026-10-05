# `--changed` vs play-freshness: why two plays were listed by one and not the other

Read-only investigation. No behaviour changed.

## Verdict

Neither judgement is wrong. They were asked about two different checkouts: the host
checkout's HEAD moved by four commits between the moment `run.bash --changed` drew up
its list and the moment play-freshness ran.

- `run.bash --changed` decides its list **once, before the "Run them now?" prompt**. The
  run's log directory is stamped 13:00:10 UTC. The F44 reflog puts HEAD at `32e62d5c`
  then (12:43:34). `2165b111` (13:06) and everything after it came later.
- The run then waited at the prompt. Its log was last written at 15:43:49 UTC, so the
  ten plays ran in the last minutes before that.
- Meanwhile the container committed into the host's own checkout. `2e10f0dc` (14:00:30)
  merged the Plan 00156 branch, and that merge brought `8965aa3b` and `fae7c99c` onto F44.
  Those two commits are the only commits between `32e62d5c` and `07aad51c` that touch
  anything under `playbooks/imports/`, and the two files they touch are
  `play-speech-to-text.yml` and `play-lxc-install-config.yml`
  (`git diff --stat 32e62d5c 07aad51c -- playbooks/imports/`).
- The 00109 acceptance ran next, at about 15:44 with HEAD at `07aad51c`. Its check [5]
  and the login report both judged those two plays as `X..07aad51c`. X is the commit each
  play last ran from, which is at or before `32e62d5c`.

So "meta-deploy ran `--changed` from `07aad51c`" is not quite right. The meta-deploy
*started* at `32e62d5c`, the commit that put this PLANS list in place. `07aad51c` was the
checkout by the time the plays ran and the acceptance read the ledger.

## The ledger content that fits both outcomes

Both readers fold the same file the same way (`ledger.fold_latest`, latest `finished`
per play). For each of the two plays, the latest record has:

- `outcome: ok`, `dirty: false`
- `commit` = some X that is an ancestor of `32e62d5c`, does not contain `8965aa3b`, and
  in which the play file, and every input `affected_plays.play_inputs` maps for it, has
  the same content as at `32e62d5c`. The speech-to-text play most likely last ran from
  the Plan 00148 deploy in the 12:21 UTC meta-deploy.

With that record:

- **changed_plays at 32e62d5c:** `git diff --name-only X` against the working tree names
  none of the play's inputs, so it is `current` and not printed. That is correct for that
  checkout.
- **check_freshness at 07aad51c:** `git log X..HEAD -- <play>` returns `fae7c99c` and
  `8965aa3b`, so it is `stale`. That is correct for that checkout.

I reproduced the second half in a scratch ledger. The two plays recorded clean and `ok`
at `32e62d5c`, then judged by `changed_plays --all` against today's checkout, are both
`PLAY stale`. `play-basic-configs.yml` from the same commit is `current`. So the judge
**does** report these plays once it is shown the moved checkout.

## Candidate causes ruled out

| Candidate                                                       | Finding                                                                                                                                                                                                                                                                                                                          |
| --------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| The judge only considers plays that `playbook-main.yml` imports | No. It judges every ledgered play under `playbooks/imports/`. `main_order` only sorts them, and unimported plays go last. Six of the ten plays it ran are optional plays.                                                                                                                                                        |
| The two sides compare against different refs (HEAD vs origin)   | No. check_freshness fetches, but it compares `X..HEAD`, never the remote ref. changed_plays compares X against the working tree. Both use the local checkout.                                                                                                                                                                    |
| A dirty run, or a run from a worktree, is treated differently   | A dirty record is always `RUN` in the judge. A worktree run records that worktree's HEAD (`repo_root_from` the plugin's own path). For the two to disagree on the **same** HEAD, X would need these plays' current content without `8965aa3b` in its ancestry. No such commit exists among the reachable or unreachable commits. |
| A cache                                                         | Only `diffs[commit]`, which lasts one invocation.                                                                                                                                                                                                                                                                                |

## Where the two judgements really do differ (for the docs)

They are **not** the same judgement, though no doc says they are. On one fixed checkout
they can still disagree in both directions:

1. **Inputs.** freshness watches only the play file. `--changed` also watches every file
   the play deploys (templates, `files/`, helper packages). A change to
   `files/home/.local/bin/vmtest` alone makes `play-vm-test-lab.yml` `RUN` in
   `--changed` and fresh in play-freshness. That is why this run's ten plays were mostly
   plays the freshness check had not flagged.
2. **History vs content.** freshness counts every commit that touched the play file in
   `X..HEAD`, even when the content ends up byte-identical (a revert, or a play that last
   ran from a branch commit that was later merged as a different commit). `--changed`
   compares content, so it says nothing in those cases. `freshness.classify` documents
   this on purpose.

The doc that can mislead someone is `docs/playbooks.md`, in the
`play-fedora-desktop-panel.yml` section: *"The label carries a count when the freshness
check has marked any play not fresh, for example 'Re-run a play… (2 changed)'"*. The row
opens the `--rerun` menu, whose `*` marks come from `changed_plays`. So the "(N changed)"
count and the number of `*` rows can differ, for reasons 1 and 2 above. Proposed wording,
after that sentence:

> The count comes from the freshness check, which watches only each play's own file. The
> menu's `*` marks come from the `--changed` judgement, which also watches what the play
> deploys, so the two numbers can differ.

## What a person saw, and the gap behind it

A person saw `--changed` say "10 plays", run them all green, and the status report say,
minutes later, that two other plays had changed. Both were true. The list belonged to the
checkout as it was almost three hours earlier.

The gap is in `run.bash`, not in either judgement: **the list is not re-judged after the
prompt, and nothing notices that the checkout moved while it waited.** The module's own
rule is that "a partial list offered as the whole one would leave plays silently unrun".
That is what happened here, only through elapsed time instead of a failed git call. On
this project the checkout moves often, because the container commits into the host's own
checkout.

Proposed fix (not done here, see below). In `run.bash`'s `--changed` (and `--rerun`) path,
record `git rev-parse HEAD` and a hash of `git status --porcelain` before calling the
judge. Compare them after the confirmation, before `play_batch_run`. If either changed,
run the judge again and show the new list and the question again, or stop with "the
checkout moved while you were deciding; run `./run.bash --changed` again". Test first in
`scripts/test-run-bash-changed.bash`: a fixture repo where the stub `confirm` makes a
commit that changes a ledgered play, then an assertion that the run does not go ahead on
the old list. The `--yes` work on branch `run-bash-changed-yes` (`db49e5b6`) mostly
closes the window for meta-deploy, because nothing waits at a prompt. HEAD can still
move during a long batch, though, and an interactive `--changed` or `--rerun` has the
full window.

I did not implement it because the main checkout has staged, uncommitted edits to
`run.bash`, `scripts/test-run-bash-changed.bash` and `run-changed.bash` from work in
progress. A parallel edit to the same lines would collide. The brief also asked for a
proposal, not a behaviour change, when the judgement turned out not to be at fault.

## Follow-up for the host

No fix is needed for this finding. Running the two plays clears it: the next `--changed`
at the current HEAD lists them (confirmed with the scratch ledger above).
