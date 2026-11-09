# Task 4.7e: an old-range removal already gone is done, not a failure

The host run of Task 4.7d (`old-range-apply.bash` at 9bd2a6528) failed in `old-range remove`:
5 images of the remove list ended "image not known", untagged parents podman had removed with
an image removed earlier in the run. `removeAll` counted them as failures, the command exited
1, and the play stopped before the ownership probe, the marker and the timer.

## What each step did with an absent decided item, before the fix

- `plan`: every decided key that is no item of the fresh triage was refused ("is decided but is
  no item of the store now; triage again"). On a rerun the fresh scan finds no old-range file:
  every removed item is gone, and every re-owned one is no longer old-range, so every decision
  was refused and the rerun stopped at `plan`, before remove.
- `stillSafe`: a container to remove that podman does not list was refused; an absent image or
  volume was not looked at.
- `removeAll`: any failed removal was a failure (exit 1), whatever the cause.

## The fix (O11 and O15 of the package doc, `prune.go`)

- `internal/engine`: `Engine.StoreHas(ctx, kind, key)` is podman's own `image exists`,
  `volume exists`, `container exists --external`: exit 0 there, exit 1 gone, any other end an
  error (never read as gone); docker gets `ErrPodmanOnly`. Tested through `execx.Fake`.
- `remove`: first, each step is asked `StoreHas`; one podman does not have is named
  "already gone: KIND NAME", dropped and not logged (the dry run too). `stillSafe` then judges
  only what is left. A removal that fails (other than P6a, which still stops the run at once) is
  asked again: gone means "removed with an earlier item: KIND NAME", logged as any removal;
  still there, or podman cannot say, the failure stands as before. podman's error text decides
  nothing. The plan's done list is printed too.
- `plan`: for each decided key that is no item now, the store is asked whether it still has it
  (podman `StoreHas` for image, container, volume; c/storage's layer records for a layer; the
  store's own directory, by `Lstat`, for a path). A remove the store does not have is "already
  gone", a reown it still has is "already re-owned" (it holds no old-range file now); both go in
  the plan's new `done` list and need no step. Anything else is refused as before.

## Decided reown items that are absent

Kept as today: a reown podman does not have is refused ("triage again"), and so is a leave
that is no item now, and a remove podman still has. Only a reown still present with no
old-range file changed (refused before), because otherwise every rerun is blocked at `plan`.

## Tests

Red first: the engine and plan tests did not compile before `StoreHas` and `Plan.Done`
existed. The remove tests were written before the code but not run red on their own.

- `TestRemoveTakesAnItemAnEarlierRemovalTook`: the host run; BASE's rm fails after CHILD's took
  it: exit 0, "removed with an earlier item", BASE logged.
- `TestRemoveRerunWhereEverythingIsGonePasses` (yes and dry run): exit 0, nothing removed or
  logged, each named "already gone".
- `TestPlanRerunOfCarriedOutDecisionsPasses`: plan over a carried-out store plans nothing and
  lists done; remove with that plan passes.
- `TestRemoveFailureOfWhatIsStillThereStands`, `TestRemoveNamesARefusalAndFails`: a real
  failure still exits 1. Also `TestRemoveStopsWhenPodmanCannotSayWhatItHas`,
  `TestPlanRefusesADecidedItemThatIsNotDone`, `TestStoreHasAsksPodmansExists`.
- Package coverage: containerimages 94.2%, engine 99.4%. The layer and path branches of
  `presentNow` have no test of their own.

## Defence Before Fix

Not a rule candidate: no static detector can tell an idempotent removal from a fragile one.

## Host

`meta-deploy.list` already queues `00235-.../old-range-apply.bash`, which builds
`container-images` from the checkout; once this commit is merged, the next `meta-deploy.bash`
run reruns the apply with the fix. Nothing new is queued.
