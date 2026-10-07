# shellcheck shell=bash
# _h2-docker-image.inc.bash — the image triage H2 runs its docker client in, one copy for
# triage.bash (its default --docker-image) and deploy.bash (which pulls it: the triage pulls
# nothing). busybox 1.37.0, pinned by its multi-arch index digest; it has sh and nc.
# Sourced, never executed: no shell options, no `exit`. Exported because nothing in this
# file reads it (ShellCheck SC2034), and it is a public image reference.

declare -rx H2_DOCKER_IMAGE="docker.io/library/busybox@sha256:bdf57e528e45e4433820e045b29b4597825a1c9e38353532d90a01445013f82e"
