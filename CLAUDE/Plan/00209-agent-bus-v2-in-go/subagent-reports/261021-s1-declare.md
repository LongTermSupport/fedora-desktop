# Plan 00209 stage 1: ccy-teams' v1 reads in-process (coordinator's report)

Branch feat/bus-v2-s1-declare, not pushed. Review: [261021-s1-declare-review.md](261021-s1-declare-review.md)
(round 1 FIX-FIRST, round 2 MERGE). Builder reports: [261021-s1-task2.md](261021-s1-task2.md),
[261021-s1-task34.md](261021-s1-task34.md).

## Commits

| Commit   | What                                                                                                    |
| -------- | ------------------------------------------------------------------------------------------------------- |
| b6404cef | bus/declare (V1-V23), parity 2,056 cases against declare.py; fixtures kept byte-exact (`-text`)         |
| 86933d71 | bus/seat and agentbus.SeatList in-process (V30-V35); teamtest and the bash suite read real seat folders |
| 462d0b88 | internal/declare answers every read in-process; tests read real host_vars files                         |
| f8e2dfd3 | V40 (one `declare status` reused while its inputs are unchanged) and V41 (the open shares its reads)    |
| 26ea1aa8 | V42: the open and a redraw pinned on the processes they start                                           |
| 267069c3 | review round 1 fixes: one edit-invalidation wiring, the full process map, agent_bus_lxc in the key      |

## Task 1: the test world

The fakes of `agent-bus seat list` became real on-disk state. teamtest gives the reader real
seat folders (`AddSeat`, `HoldSeat` with a flock on a descriptor of the test's own,
`RemoveSeat`, `BreakSeat`). Populated's seats are as its fake printed them. The restart and
shutdown machines hold and free real locks. The hot rig's keeper follows the desired file
whenever the seats are read, so the rig syncs its folders through the `Client.ListSeats` seam
and then the real reader reads them. The unreadable and crash cases are real files the reader
refuses (`{}`) or crashes on (nesting 30000 deep). The process allowlists no longer admit a
seat list. The bash suite holds a seat lock with a background process, frees it when a session
ends, and its fake agent-bus refuses `seat list`. Pinned behaviour is unchanged (383 passed).
`agentbus.ParseSeats` and `Seat.Self` are deleted: nothing reads them, and v1 never set self.

## Task 5: an open, timed in this container (the suite's fake world)

`untracked/scratch/261021-s1-timing.bash` builds each revision's binary and runs its own suite's
fakes, 10 runs each, twice in opposite order:

| Run                        | before (d342732b) | after (HEAD)  |
| -------------------------- | ----------------- | ------------- |
| picker open, then Esc (ms) | 645-857           | 138-163       |
| `--list`, one Collect (ms) | 116-142           | 111-196, same |

In the fake world `agent-bus` is a bash stub, so the "before" open does not include the cost of
v1's real Python seat list, nor the host's slower Python. The host saving is larger than shown
here. `--list` is one pass with one Python run both before and after (`overview` became
`status`), so it is not expected to change.

## Host check to queue (the coordinator's)

After ccy-teams is rebuilt on the host by the play that builds it, on the live teams:

1. Time `ccy-teams` (open, then Esc) and `ccy-teams --list` against the deployed old binary's
   numbers (9-23 s today).
2. Compare the old and new binaries' `ccy-teams --list` output on the live host_vars and live
   checkouts. It should be equal byte for byte, apart from the SPEC-view departures B-S1-B-S6.
3. Check that an open starts one Python run, `declare status`, and no `agent-bus seat list`.

Not done here: the host runs, and the IPv4-mapped address rows (B-S5), which need the host's
Python 3.13 to compare.
