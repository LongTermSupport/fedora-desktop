# `vmtest refresh-base all`, read from the code (Task 2.7)

Read from `files/home/.local/bin/vmtest` (`cmd_refresh_base`, `cmd_build_base`,
`build_seed_fast`, `build_install_full`, `freshness_verdict`, `cmd_freshness_status`),
`helpers/vmtest/freshness.py`, `freshness_gate.py` and `retention.py`. Nothing was run.

## Prompts and sudo

None on the host. The libvirt connection is `qemu:///session` (rootless). `ssh` and `scp`
use `BatchMode=yes` with the lab key. `virt-install` runs with `--noautoconsole`. `gpg2`
only verifies, in a temporary homedir. `curl`, `blkid` on an ISO file, and `qemu-img`
need no root. The desktop LUKS passphrase is answered by `helpers.vmtest.serial_console`.
Every `sudo` runs inside the guest over BatchMode ssh.

The `read`s in the path all read here-strings or process substitutions. `guest_ssh` does
not pass `-n`, so ssh can read the caller's stdin, but nothing in that path reads stdin
after it.

## Disk use

Peak use comes when the new base is built beside the old one, and the old one is replaced
only at the end:

- The build disk is `bases/<name>.build/disk.qcow2`: a reflink copy of the Cloud image
  for a fast base, a new 20G qcow2 for a full one.
- When the build completes, `mv -f` moves it over `bases/<name>/base.qcow2`.
- At peak, the old base, the growing build disk and the cached media in `images/` all
  exist together. A media download also goes to `<file>.partial` beside the old copy.
- Before building, `retention floor --step rebuild` refuses the rebuild unless there is
  2 × the base size + 2 GiB free (`needed_for_rebuild`).
- After the run there is one base per name, plus the cached media in `images/`, which
  refresh-base keeps.
- A failed build keeps its `.build` directory for diagnosis, and the old base is left
  untouched.

## Findings (not fixed; outside this task)

1. `cmd_build_base` never resets `BUILD_OK`. Under `refresh-base all`, when a later base
   fails after an earlier one succeeded, `teardown_build` sees `BUILD_OK=1` and deletes
   the failed build's directory instead of keeping it for diagnosis.
2. Under `refresh-base all`, the trap runs only at exit. Each earlier base's
   `<name>.build/` directory (console log; its disk has been moved out) and its probe temp
   file are therefore left behind. Only the last ones are removed.
3. Straight after a rebuild, `freshness-status` can still read `refresh` if the guest's
   mirror lagged (`refresh_state=incomplete`). It can also read `current` with
   `degraded=true` if the revision could not be read. Both exit 0. Check the decision
   field, not the exit code.

The plan script (`refresh-vm-bases.bash`) was dropped on the owner's direction. The owner
runs `vmtest refresh-base all` when they choose.
