# Search: leakcheck.Check registered more than once on one t

Module: <repo>/untracked/worktrees/bus-v2-n3e/src-go (read only). I did not open qa/rules/ and did not run the detector.
The dispatch prompt did not contain the rule or its pattern; I searched from the class description alone.

## Direct callers of leakcheck.Check (whole module)

Only two:
- internal/teamchat/member/engine/engine_test.go:289, in newWorldWith. First statement, before any Cleanup.
- internal/teamchat/member/link/link_test.go:222, in newKeeperWorld. First statement, before newHome(t) and the other cleanups.

Every other leakcheck use is `leakcheck.Tests(m)` in a TestMain (matrix, client, engine, hook, inbox, units leak_test.go; keeper/osproc_test.go; link/process_test.go; wiring/e2e_main_test.go). That is package-level, takes no t, and is not in the class. No dot-import or alias of the package exists.

## Transitive helper chains to Check

Engine package:
- newWorldWith -> Check
- newWorld -> newWorldWith
- newDeliveryWorld -> newWorldWith
- newProofWorld -> newDeliveryWorld
- quietWorld -> newWorld
- Direct newWorldWith calls in tests: hookview_test.go:171 and :197, deliverloop_test.go:20, proof_test.go:489. Each is once per test.

Link package:
- newKeeperWorld -> Check
- newLinkWorld -> newKeeperWorld
- newHookWorld -> newLinkWorld
- Direct newKeeperWorld calls: failpoints_test.go:27 and :70, process_test.go:102 and :118. Each is in its own test.
- newServerWorld, newFactsWorld and newHome do NOT reach Check.

## List 1: instances (second reaching call on the same t)

Engine package, all confident:
1. engine/deliver_test.go:289, TestLiveNeedsEveryFact. Chain: newDeliveryWorld(t, DeliveryWait) -> newWorldWith -> Check. First call at :270. This is the example in the dispatch.
2. engine/deliver_test.go:930, TestAnItemAwaitingProofIsNotPending. Chain: newDeliveryWorld -> newWorldWith -> Check. First call at :913.
3. engine/proof_test.go:383, TestMissingSurvivesARestart. Chain: newDeliveryWorld -> newWorldWith -> Check. First call at :359 via newProofWorld -> newDeliveryWorld, so two different constructors reach Check on one t.
4. engine/proof_test.go:629 and :652, TestBeginRunReturnsAnEarlierRunsEntries. newProofWorld is called three times on one t (:613, :629, :652). Chain: newProofWorld -> newDeliveryWorld -> newWorldWith -> Check. The second and third reaching calls are :629 and :652.

Link package, confident (loop form):
5. link/link_test.go:687, TestRemoveAnswersThenLeaves. newLinkWorld(t) is called inside a plain `for _, farewell := range` over 4 values, with no t.Run. Chain: newLinkWorld -> newKeeperWorld -> Check (link_test.go:222). Check is registered 4 times on one t; iterations 2 to 4 are the instances.

## Checked and not instances

- Constructor calls at engine/proof_test.go:140 and :462, and engine/recv_test.go:71 and :169, are inside `t.Run(name, func(t *testing.T){...})` in a loop. Each subtest has its own t.
- Every other Test function in engine/ and link/ reaches a constructor exactly once, by a per-function count over all _test.go files in both packages.
- quietWorld is called once per test (statuspace_test.go:41, :65, :105, :123 are four separate tests). newHookWorld is also once per test.
- Packages with only TestMain use of leakcheck never reach Check.

## List 2: goroutine-starting setup with a Cleanup registered BEFORE the t's first Check

None found.
- newWorldWith: Check is the first statement; the root, keeper, engine goroutine and shutdown cleanups follow it.
- newKeeperWorld: Check is first; newHome(t), the keeper and connect's goroutine follow. newLinkWorld sets up nothing before newKeeperWorld.
- newDeliveryWorld builds `&dworld{dial: newFakeDialer()}` before newWorldWith. I did not open newFakeDialer's body, so whether it starts a goroutine is unverified. No Cleanup is registered there.
- newProofWorld and quietWorld call their world constructor first.
- A scan of every Test function for Cleanup, go statements, Start(, Serve, listen and New( before the first constructor call found only t.Run closures (own t).
- TestDeliveryOverTheInboxSocket (engine/deliverloop_test.go:20) builds its world first; fakeinbox.Start and its Cleanup come after, which is the safe order.
- Tests in link that start things without a Check world (parent_test.go reexec.Start, server_test.go newServerWorld) never reach Check.

## What each technique found

Text search (grep and awk over function bodies) found:
- The two Check sites and the TestMain users.
- The constructor call lists and per-function counts, which gave instances 1 to 4.
- Indented constructor calls (loop or closure candidates), which pointed at instance 5 and the t.Run cases.

Reading found:
- That link_test.go:687 is a plain loop, not a t.Run, so one t gets four registrations. A per-function count of 1 hides it.
- That the t.Run-nested constructors have their own t.
- That instance 3 mixes two constructors.
- The helper chains, and that Check is the first statement in both helpers (list 2 clean).

## Searches run

- `grep -rn "leakcheck\."` and `grep -rn 'teamchat/leakcheck"'` over all .go files outside qa/rules.
- Constructor-name grep in engine/ and link/.
- An awk per-function counter of constructor calls (newWorld, newWorldWith, newDeliveryWorld, newProofWorld, newKeeperWorld, newLinkWorld, newHookWorld, newFactsWorld, newServerWorld*, quietWorld).
- An awk listing of constructor calls inside non-Test functions.
- `grep -P` for constructor calls indented two tabs or more.
- A scan for Cleanup, go and Start before the first constructor in each Test function.
- Reading of the helper bodies and of the tests above.

Forms looked for: a repeated constructor in one function, a constructor in a loop, a second helper reaching Check, a nested helper chain, and a dot-import or alias of leakcheck.
