## 13:26 · action · —   — Task 4.9 green: the store-clean gate

Task 4.9, green. prune.go gains P9 and storeCleanName (container-images.store-clean, beside the
log, lock and stamp names); replaced.go gains R7. prune --yes checks the marker after taking
its run lock and, without it, says the one line on stderr and exits 1 before opening the log;
the dry run notes it and lists as before; replaced checks it once some recorded image would go,
before the build-lock probe, and exits 0. A marker that cannot be stat'ed is a failure.

play-podman.yml names the state directory once (container_images_state_dir, also used for the
success stamp) and the marker (container_images_store_clean), each pointing at the Go; after the
ownership probe it makes the directory (0750, as the Go makes it) and writes the marker with the
probe's time (0644) when no foreign-owned file was found, or removes it when one was, before the
timer task and the deliberate failure.

scripts/test-ccy-image-build-lock.bash did not sandbox HOME, so its self-prune runs appended to
the real ~/.local/state/fedora-desktop/container-images.log. HOME is now the sandbox's, set
after the Go builds, and each reset_world writes the marker; a new case removes it and checks
that a rebuild succeeds, removes nothing and says why.

go test -race on containerimages (coverage 95.3%) and cmd/container-images, golangci-lint run
and fmt clean, go test ./internal/ccylaunch/... and the lock suite pass.
