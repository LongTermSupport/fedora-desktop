# Unblocking session, 2026-10-28

Answer by number, in any order, by voice. **"All recommended" accepts every recommendation
below.** Only these questions need you. I have decided everything else (part C), and you can
overrule any of it.

## A. Questions only you can answer

1. **Sub-agents acting as coordinator.** Add a hooks-daemon rule that applies only inside a
   sub-agent. It refuses:

   - the bus commands that speak (`pingbus say`, `send`, `report`, `ask-human`, `thread`);
   - team changes through `ccy-teams`;
   - starting further sub-agents.

   Read-only pingbus stays allowed. A sub-agent's only channel is then its final reply to the
   coordinator. Coordinators keep their sub-agents. **Recommended: yes.** The alternative is
   no sub-agents for coordinators, with every worker a separate LXC or ccy session.
   If you have the session and time of the incident you saw, tell me, and I'll reproduce it
   first.

2. **Two LXC members in one team** (one coordinator, one worker). The code supports it
   (`user@container` identity, a seat per entry, a Role step in `lxc-add`), but it has never
   been accepted on the host. **Recommended: yes**, seat 2 proves it with two throwaway
   members.

3. **Go lint record (00191 Q5).** Sign `src-go/qa/record.toml` (`high_water = 27`, decided by
   you, today). The trust tier sets how often it is reviewed: **medium** means a review every
   30 days, **high** every 90 days. **Recommended: sign, medium.** If you say "sign", I write
   the table in your name, from your words here.

4. **Whisper model manager stays Python (00234 Q6).** This is an exception to the Go-port
   rule: it is a thin wrapper over CTranslate2 and huggingface_hub, which have no Go
   equivalent. **Recommended: yes, keep Python.** The same reading lets host-health keep its
   two new podman probes in Python (00235 Q5).

5. **Speech-to-text on a consumer install (00228 Task 6.5).** The Claude post-processing
   needs ccy's token pool, which a consumer install does not have. **Recommended: no
   post-processing for consumers, and hide those menu items there.** Revisit if a real
   consumer asks.

6. **Podman volumes (00235).**

   - 30 volumes are dangling (no container uses them). **Recommended: keep and report them;
     you review the list when the timer prints it.**
   - One volume is orphaned: its data is owned by a uid outside your current subuid range, so
     no user namespace can read it, and it makes `podman system df` fail. Is it wanted?
     **Recommended: remove it through a play step** (root, one-off).

7. **00201 step 08.** May the host agent rename `~/.config/pingbus/element-accepted`, and put
   it back, for the step-08 check? **Recommended: yes.** Otherwise it is a hand check in part B.

8. **ssh-with-key refuses** when your ssh config gives every host an IdentityFile. This only
   matters if a read-only `ssh -G` by seat 2 finds such a line. **Recommended: the refusal
   stands, and you scope those lines to their hosts.** I'll ask seat 2 to check first.

## B. Hand checks, grouped for one sitting

Steps are in the cited specs. Say "later" for any group.

- **At the desk:**
  - 00198: Nautilus opens a new window on the current workspace (Q4).
  - 00234: the Manage Whisper Models screen.
  - `open` H2 and H3; `reclaim` H5 (baobab).
  - 00201 step 11 (last, because it signs out Element).
  - 00141 Task 4.4: the panel's changed-plays row.
  - 00203: the M3-M6 owner steps.
  - setup-rclone H1-H4 (a real remote).
- **Speakers and microphone:**
  - `open` H5: Ctrl-C in mpv.
  - wsi H1: dictate and paste.
  - wsi H2: Escape discards the recording.
- **Your accounts:**
  - gh-account-setup H2 and H5 (a real new account).
  - vmtest H6 (a throwaway GitHub account).
  - ssh-with-key H5: your key and host, and `-A` with `git push`.
  - 00177 Task 9.5: gh login in a real LXC container, then `lxc-add`.
- **Network** (choose a quiet moment): nord H4-H7; ftp-camera H5 (hotspot).
- **Camera:** ftp-camera H1, H3 and H4.
- **Reboot:** 00203 B1 and B2 (restore after a reboot); nvidia-status H5 (at the next driver
  update).

## C. Decided by me (overrule any by number)

01. **Wind-down compaction (00203 Task 3.20):** done by typing `/compact` into every kind
    (ccy, cc, LXC), per your note today. The hooks-daemon supervisor route becomes a later
    improvement, not a precondition. Being built now.
02. **00194 H7:** not applicable, since no launch omits cross-session inbound (Q2, closed).
03. **00192 Task 3.7:** a group_vars change is not counted as an input of every play. A
    follow-up will name the plays that read each changed variable (Q3, closed).
04. **00177:** turning a seat's restore OFF asks first (default No); turning it ON asks
    nothing.
05. **00228:**
    - On the developer path, optional plays go on after one fails. A summary is printed at the
      end, and the exit is non-zero; main plays still stop at the first failure.
    - Decisions 4-9 stand on their defaults.
06. **00203 Decisions 5-24:**
    - Every default stands, except two, where I take the alternative.
    - D5: removing a coordinator while the homeserver is down is refused without
      `--coordinator=demote`.
    - D6: one definition of "live" in ccy-teams and agent-bus.
07. **00235 Q1-Q4:**
    - A project image unused for 30 days is removed.
    - Health is flagged under 10% or 50 GiB free, over 50 dangling images, or the timer
      silent for 3 days.
    - The first timer run clears the backlog without a separate dry run.
08. **Play ledger gap:** `run.bash --changed` will name the plays with no record since the
    ledger was cleared, rather than treat them as never run. Seat 2 then runs the installed
    ones once, by name.
09. **The secret guard misreading nmcli's VPN secrets setting:** keep the workaround, and file
    an upstream issue. The guard is not weakened locally.
10. **The prune question (Q7):** closed, since you said yes in person and it ran (107.5 GiB
    freed).
