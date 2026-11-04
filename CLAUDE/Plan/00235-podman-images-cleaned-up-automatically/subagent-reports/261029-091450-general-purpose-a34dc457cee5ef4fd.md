All five asks are built and committed in one commit. qa-changed passes on the final tree; qa-all was not run, as instructed.

**Branch:** `agent-a34dc457cee5ef4fd-3b9b09f2`, fast-forwarded from `plan/00192-ssh-with-key-merge` first. **Commit:** `e6ef91ba2`. Not pushed or merged.

**(b) `podman system reset`:** the opt-in `podman_storage_reset` block and its variable are gone from `play-podman.yml`. No docs or tests named it. I added no QA rule: none of the existing rule homes checks a command inside Ansible YAML cheaply.

**The label:** `ccy-version=<CCY_VERSION>` now goes on every image ccy makes: the `:base` and `:latest` builds, project image builds, the in-place update, and the play's build of `:latest`. The version read moved into `tasks/claude-yolo-image.yml`; the Go front still uses it. CCY is now 3.103.0; the container version is unchanged.

**The stale rule** (`container-images stale`, in `src-go/internal/containerimages/stale.go`):
- A claude-yolo image is stale if its label is older than the installed CCY version, or it has no label.
- Its project tags are removed, whatever their age, once no container uses the image and no other image is built on it.
- `:base` and `:latest` are never removed.
- It is gated by the store-clean marker and by other ccy build locks, like `replaced`.
- **Where it runs:** `lib/image-build-prune.bash` runs it right after `replaced`, inside the build lock. So it covers every launch's build and the play's build of `:latest`, which happens right after the play installs the new ccy. A failure is reported but never fails the launch or the build.
- **Stopped containers:** no container is ever removed. Each stopped container keeping a stale image is named on stderr and logged as a `held` line in `container-images.log`. Running ones are only named on stderr. The daily timer doesn't know the installed version, so the held lines come only from these build-time runs, and a session that stays stopped is logged again at each build.

**The TTL:** the old 30-day rule for unused project images (P2) is the same idea, so it now covers every tagged image at 28 days. The daily prune removes a tagged image that no container uses and no other image is built on, once it is over 28 days old. Project images also still need their last ccy launch to be over 28 days ago.
- **Timestamp:** the image's creation time. Podman records no last use and no pull time for an image. So a pulled image's age is when it was built upstream, and an old upstream image pulled recently can go at the next prune; the cost is one re-pull.
- **Built-on check:** podman refuses to remove an image another one is built on. The prune now detects that by comparing image layers (a new `ImageLayers` call in `internal/engine`) and keeps those images.
- **One departure from "other images, 4 weeks":** an image built on this host that nothing rebuilds (a `localhost/` name that isn't a ccy project image, such as `claude-devtools`, which `ccdt` runs daily with `--rm`) is named and kept. By creation time it would be deleted while still in daily use. Putting it under the TTL is one condition if the owner wants that.

**old-range:** `old-range.bash` now takes no arguments. It runs the triage, then dry-runs the decisions file if it exists at `untracked/plan-00235/old-range-decisions.tsv`. A new `old-range-apply.bash` applies that file. Both are plain script entries. The plan, help texts and Q7 (with the owner's quote) are updated and journalled.

**Tests:** written first and seen to fail, then pass: the Go tests and the image-build-lock suite (126 passed). I re-recorded the launcher's comparison goldens; the only differences are the new label arguments and the `stale` call.

**Meta-deploy entries** (I did not edit `meta-deploy.list`):
```
# Plan 00235 Q7 (CCY 3.103.0): installs ccy with images labelled ccy-version and the stale-image
# self-prune, and container-images with the four-week TTL. Its build of claude-yolo:latest runs
# the new rule at once: project images an older ccy built (or unlabelled) that no container uses
# are removed (each rebuilds on its next launch); stopped containers keeping stale images are
# named and logged as held, never removed. Nothing is removed while the store is not confirmed clean.
playbooks/imports/play-claude-yolo.yml
# Plan 00235 Task 4.7d, read only: the old-range triage; once
# untracked/plan-00235/old-range-decisions.tsv exists, also its dry run. Prompts for sudo.
00235-podman-images-cleaned-up-automatically/old-range.bash
```
Then, only after the host agent has written the decisions file and read the dry run:
```
# Plan 00235 Task 4.7d, CHANGES THE STORE: play-podman.yml applies
# untracked/plan-00235/old-range-decisions.tsv (root re-owns, podman removes, each logged),
# then its probe judges the store.
00235-podman-images-cleaned-up-automatically/old-range-apply.bash
```
The `play-podman.yml` changes go out with the existing 00235 plan entry.

**Report:** `<worktree>/untracked/scratch/261029-stale-images-report.md`