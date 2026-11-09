#!/usr/bin/env bash
# nvidia-status.bash — reports whether the NVIDIA driver installed by play-nvidia.yml is built,
# loaded and working (Secure Boot and MOK, packages, modules, nvidia-smi, OpenGL, Vulkan, VA-API,
# the kernel's NVIDIA errors). `--check` prints nothing and exits with the verdict; --help.
#
# The tool is Go (src-go/internal/nvidiastatus, command src-go/cmd/nvidia-status);
# lib/go-tool.bash builds it from this checkout on each run, so it needs no play first, and the
# command's exit code is this script's. Specification: CLAUDE/Plan/00192-.../SPEC-nvidia-status.md.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source-path=SCRIPTDIR
# shellcheck source=lib/go-tool.bash
source "$SCRIPT_DIR/lib/go-tool.bash"
run_go_command nvidia-status "$@"
