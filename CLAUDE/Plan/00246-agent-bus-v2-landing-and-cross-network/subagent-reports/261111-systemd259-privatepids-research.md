# systemd 259: DynamicUser + StateDirectory + PrivatePIDs fails with 226/NAMESPACE

Research only. No tracked file edited apart from this report, and nothing run on a host.
Placeholders: `<team>` for a team id, `<host>` for the deploy host.

## Verdict

This is a known upstream systemd bug that is **still open**: systemd/systemd#41014, "Failed to
write UID map: Operation not permitted" with `StateDirectory`, `DynamicUser=yes`,
`PrivatePIDs=yes` (https://github.com/systemd/systemd/issues/41014). The labels are `bug`
and `pid1`. It was opened against 258.5 on Fedora 43 x86_64 (kernel 6.18). Later comments add
260 on Arch Linux ARM aarch64 and 262 on NixOS aarch64. No fix is merged: `userns_acquire()`
on `main` is unchanged (see 1.3). It is not specific to aarch64, the kernel or SELinux. Every
systemd from 257 on has both features, and the bug is in how they are combined.

**Recommendation: remove `PrivatePIDs=yes` from `baseSandbox`**
(`src-go/internal/teamchat/host/sandbox.go`). That fixes the homeserver unit and the restore
staging unit. Keep it on `teamchat-tls@`, which does not hit the bug. With a distinct uid per
team, `ProtectProc=invisible`, an empty capability set and `NoNewPrivileges=yes`, the PID
namespace adds very little security (section 3).

## 1. Why the three together fail

### 1.1 The two features, both new in 257

- systemd NEWS, "CHANGES WITH 257"
  (https://raw.githubusercontent.com/systemd/systemd/main/NEWS) says: "When DynamicUser= is
  combined with StateDirectory=/RuntimeDirectory=/CacheDirectory=/LogsDirectory= and ID mapped
  mounts are available on the referenced path, the data in there is now preferably made
  available by establishing ID mapped from the "nobody" user to the dynamic user, rather than
  via recursive chown()ing." The PR is #34078, "Add support for id-mapped mounts to Exec
  directories", merged 2024-11-02, commit eae5127246
  (https://github.com/systemd/systemd/pull/34078). This explains why the state directory is
  owned by 65534:65534.
- The same NEWS section adds `PrivatePIDs=`. The PR is #34940, merged 2024-11-05
  (https://github.com/systemd/systemd/pull/34940).
- systemd.exec(5) documents that these directories are owned by nobody: "the specified
  directories will be owned by "nobody" in the host namespace and will be mapped to ... the
  service's UID/GID in its own namespace. For backward compatibility, existing directories
  created without id-mapped mounts will be kept untouched."
  (https://github.com/systemd/systemd/blob/main/man/systemd.exec.xml, around line 1587)

### 1.2 The mechanism, read from the v259 source

1. `setup_exec_directory()` decides whether to use an idmapped mount. If the uid and gid are
   not 0 and the filesystem supports idmapping, two cases set `idmapped = true`: the directory
   is already owned by nobody:nobody, or it is a private (DynamicUser) directory owned by
   root, which is then chowned to nobody. In every other case it does the old recursive chown
   with `idmapped = false`.
   (https://github.com/systemd/systemd/blob/v259/src/core/exec-invoke.c, around lines
   3197-3240)
2. `setup_private_pids()` forks with `FORK_NEW_PIDNS|FORK_DETACH`. The rest of `exec_invoke()`
   runs in the child, which is PID 1 of the new namespace. This happens *before*
   `apply_mount_namespace()` (same file, around lines 4716-4745: "If PrivatePIDs= yes is
   configured, we're now running as pid 1 in a pid namespace!").
3. In `apply_one_mount()`, every idmapped entry calls `userns_acquire(uid_map, gid_map)`. On
   failure it logs exactly "Failed to allocate user namespace: %m"
   (https://github.com/systemd/systemd/blob/v259/src/core/namespace.c, around lines
   2087-2117).
4. `userns_acquire()` forks `(sd-mkuserns)` with `FORK_NEW_USERNS` and then writes
   `/proc/<pid.pid>/uid_map`, `/setgroups` and `/gid_map`
   (https://github.com/systemd/systemd/blob/v259/src/basic/namespace-util.c, around lines
   552-587).
   - `pid.pid` is the pid in the **new** PID namespace. It is 2, as the debug log in #41014
     shows: "Successfully forked off '(sd-mkuserns)' as PID 2".
   - `/proc` at this point is still the **host's** procfs. The namespace's own proc is
     mounted only under `/run/systemd/mount-rootfs/proc` and becomes `/proc` only after the
     root switch.
   - So the write goes to the host's PID 2 (kthreadd), and the kernel refuses it.
5. The error reaches `exec_invoke` as "Failed to set up mount namespacing:
   /var/lib/private/...", and the exit status is 226/NAMESPACE.

This matches all four bisect results:

| Units tested                                    | Result | Why                                                         |
| ----------------------------------------------- | ------ | ----------------------------------------------------------- |
| DynamicUser alone; DynamicUser + StateDirectory | starts | no PID namespace, so `/proc/<pid>` is right                 |
| DynamicUser + PrivatePIDs                       | starts | no exec directory, so no idmapped mount                     |
| DynamicUser=no + StateDirectory + PrivatePIDs   | starts | uid 0, so `uid == 0` takes the chown path with no idmapping |
| DynamicUser + StateDirectory + PrivatePIDs      | fails  | idmapped mount set up inside the PID namespace              |

Agreement on the cause:

- The #41014 thread names the same cause: the pid namespace does not match `/proc`
  (https://github.com/systemd/systemd/issues/41014#issuecomment, first comment).
- I confirmed it independently from the code paths above.
- No maintainer has commented to confirm it.

### 1.3 Fix status

- `userns_acquire()` on `main` still formats `/proc/" PID_FMT "/uid_map` from `pid.pid`
  (https://github.com/systemd/systemd/blob/main/src/basic/namespace-util.c, around line 771).
  `apply_one_mount()` still calls it
  (https://github.com/systemd/systemd/blob/main/src/core/namespace.c, around line 2193).
- No PR references #41014. The only related PrivatePIDs fix is #44547 (merged 2026-11-01,
  https://github.com/systemd/systemd/pull/44547). It covers mountfsd/nsresourced peers under
  PrivatePIDs, not this bug.
- Affected: the 257, 258, 259, 260 and 262 reports, and current `main`. The v257 source has
  the same order of steps. Latest point releases: v259.11, v260.7, v261.5, v262.2
  (https://github.com/systemd/systemd/releases). Nothing on record says any of them fixes it.

### 1.4 Architecture, kernel and SELinux

- **x86_64 is affected.** The original report in #41014 was on x86_64.
- **SELinux is not the cause.** The #41014 reporter had SELinux disabled at boot and saw the
  same failure, with EPERM. On `<host>` the failure persists with the dontaudit rules off and
  with the system permissive (1.5).
- **Kernel and filesystem only decide whether systemd uses idmapping at all.** btrfs, ext4 and
  xfs support idmapped mounts on any kernel in use today.
- **One "works" report is unexplained:** systemd 260 on x86_64 with kernel 7.0, in a #41014
  comment. A likely cause is a state directory created before 257 and chowned to the dynamic
  uid, which keeps the old chown path (1.2 step 1). **Unconfirmed.**

### 1.5 SELinux and user-namespace limits: ruled out on the host

- **SELinux is ruled out on `<host>`.**
  - With the dontaudit rules off, the repro still exits 226 with the same two "Permission
    denied" messages, and logs no AVC, USER_AVC or SELINUX_ERR.
  - With the whole system permissive, it still exits 226.
  - This agrees with upstream: the #41014 reporter saw the same failure with SELinux disabled.
- **The user-namespace limit is not the cause either.** Creating the namespace works:
  `(sd-mkuserns)` is forked with `FORK_NEW_USERNS` before any map is written. The upstream
  log shows that fork succeeding, and the host's `user.max_user_namespaces` is not exhausted.
- **The cause is systemd's idmapped DynamicUser state directory combined with
  `PrivatePIDs`.**
  - It is the PID-namespace mismatch in 1.2: the `uid_map` write lands on the host's PID 2.
  - The kernel and btrfs only make idmapping available. They are not the defect.
  - The same failure is reported on 258, 260 and 262, on other kernels.
- **One debug-level run would show the step directly.** Run the failing transient unit with
  `-p LogLevelMax=debug` and the systemd log level at debug. Expect "Successfully forked off
  '(sd-mkuserns)' as PID 2" and then "Failed to write UID map: ...".

## 2. Unit-level settings that keep all three

| Option                             | Works?                    | Cost                                    |
| ---------------------------------- | ------------------------- | --------------------------------------- |
| An option to turn idmapping off    | None exists               | -                                       |
| `PrivateUsers=` (any value)        | No, by reading the code   | -                                       |
| Ordering, `JoinsNamespaceOf=`      | No                        | -                                       |
| State directory pre-owned by a uid | Yes, by the code and man  | Fragile; needs IaC to own the directory |
| that is neither 0 nor 65534        | page (documented backward | (details below)                         |
|                                    | compatibility)            |                                         |

- **No setting turns idmapping off.** Neither `main` exec-invoke.c nor systemd.exec(5) has
  such an option. The decision depends only on uid/gid, filesystem support and the existing
  ownership.
- **`PrivateUsers=`: no, by reading the code.** The idmap step runs in
  `apply_mount_namespace()`, after the PID namespace exists, whatever `PrivateUsers=` is.
  **Not tested.**
- **Ordering and `JoinsNamespaceOf=`: no.** The order is fixed inside `exec_invoke()`.
- **Pre-owned directory: yes, by the code and the documented "existing directories ... kept
  untouched".** If the directory is owned by any uid that is neither 0 nor 65534, systemd
  takes the old path: a recursive chown to the dynamic uid, with `idmapped = false`, so it
  never calls `userns_acquire`. The costs:
  - It depends on a backward-compatibility branch that upstream may drop.
  - IaC would have to chown the existing `/var/lib/private/teamchat/<team>` away from 65534
    once, and own every new team's directory before its first start.
  - Any path that recreates the directory as root brings the failure back. That includes a
    restore, a team rebuild, or `systemd-tmpfiles`.
  - It loses the `nosuid,noexec,nodev` flags that the idmapped remount adds.
  - It is a non-obvious invariant that this repo would have to test.

## 3. What v2 relies on PrivatePIDs for

### 3.1 Where the reason for it came from

- In v1 (`agent-bus-hs@`) every team ran as one shared static `User=agent-bus`.
  `PrivatePIDs` plus `TemporaryFileSystem`/`BindPaths` stopped a sibling homeserver reaching
  another's secrets through `/proc/<pid>/root`, cwd or fd, or ptrace. Sources:
  - `CLAUDE/Plan/00161-agent-team-bus-matrix/subagent-reports/261015-sec-infra-review-fable.md`,
    lines 50-58
  - `CLAUDE/Plan/00201-ccy-teams-many-teams-per-session-descriptions-and-a-picker/subagent-reports/261015-security-infra-opus.md`,
    M4
  - `CLAUDE/Plan/00209-agent-bus-v2-in-go/RESEARCH-host.md:323`
- v1 never hit this bug because it had no `DynamicUser`, so no idmapping.
- `DESIGN-v2-C-host.md` 3.2.2 (around line 187) says v2 replaces that mount trick with a
  kernel identity per team (`DynamicUser=yes`, `User=teamchat-%i`). In its own words,
  isolation "no longer rests on a sandbox detail" (DC2). `PrivatePIDs=yes` (line 272) was
  carried over as ordinary hardening.
- The design's only functional dependency on it is a side effect, not a benefit (H 5;
  `SPEC-n2g.md:14-15`, `DESIGN-v2-C-host.md:2465`, `RESEARCH-host.md:228`). As PID 1, Tuwunel
  drops a SIGUSR2 that arrives before its handler is installed. Without PrivatePIDs that
  signal kills Tuwunel and `Restart=always` brings it back. The backup already waits for the
  server to be active and ready before signalling (BT1), so either outcome is handled.

### 3.2 What PrivatePIDs still adds with distinct uids

The threats it was added for are already blocked another way:

- **`/proc/<pid>/root`, `cwd`, `fd` and `environ` of other teams:** a different uid fails
  ptrace-access checks, and `ProtectProc=invisible` with `ProcSubset=pid` hides those
  processes.
- **ptrace and kill:** these need the same uid or a capability, and `CapabilityBoundingSet=`
  is empty with `NoNewPrivileges=yes`.

What remains is small:

- **(a) PID enumeration.** It hides host PID numbers and existence, so `kill(pid, 0)` cannot
  tell EPERM from ESRCH. This is an information leak only.
- **(b) Child cleanup.** If Tuwunel dies, its own children die with it. systemd's cgroup kill
  already does this, and Tuwunel forks nothing.
- **(c) Same uid as the staging unit.** The homeserver and the restore staging unit run as
  the same uid, `teamchat-<team>`. They are never up together: `AssertPathExists=!restore.block`
  and the restore stops the server. PrivatePIDs would only matter if that invariant broke.

### 3.3 Other units carrying PrivatePIDs

| Unit                                                                                                                           | DynamicUser | StateDirectory / exec dir | Affected?                                                |
| ------------------------------------------------------------------------------------------------------------------------------ | ----------- | ------------------------- | -------------------------------------------------------- |
| `teamchat-server@` (base sandbox)                                                                                              | yes         | `teamchat/%i`             | **yes**                                                  |
| restore staging unit (`restore/argv.go` StagingArgv: whole base sandbox; `checkSandbox` requires DynamicUser + StateDirectory) | yes         | yes, same directory       | **yes**: every restore and RS3 verify fails the same way |
| `teamchat-tls@`                                                                                                                | yes         | none                      | no (the bisect's "DynamicUser + PrivatePIDs starts")     |
| restore secure unit (`SecureArgv`)                                                                                             | no          | none                      | no PrivatePIDs                                           |
| LXC member unit (`host/lxc/unit/unit.go`, `RuntimeDirectory=`)                                                                 | no          | runtime dir               | no PrivatePIDs                                           |

## 4. Fix options, ranked

1. **Remove `PrivatePIDs=yes` from `baseSandbox`; keep it on `teamchat-tls@`.
   Recommended.**
   - It fixes the homeserver and the staging unit, because the staging unit takes its lines
     from the base sandbox.
   - Security cost: 3.2 (a) to (c). These are minor, and DC2 already says v2's isolation is
     the uid.
   - Behaviour cost: an early SIGUSR2 is fatal and the unit restarts, instead of the signal
     being dropped. BT1 already handles this.
   - Docs to change to match:
     - `SPEC-n2g.md:14-15`
     - `DESIGN-v2-C-host.md:272` and `:2465`
     - the golden file, and any test that asserts PrivatePIDs or PID 1
   - Leave a comment citing systemd#41014 so it can be put back once that issue is fixed.
   - Under Defence Before Fix, the defect class is "a unit with DynamicUser + an exec directory
     - PrivatePIDs". The detector could be a check in `unitfiles` or `host` tests over every
       unit and transient argv.
2. **Pre-own the state directory (section 2).**
   - It keeps PrivatePIDs, but depends on a backward-compatibility branch in systemd.
   - The existing 65534-owned directory needs a one-time chown through IaC.
   - It breaks again silently whenever something recreates the directory as root.
   - It loses `noexec`/`nosuid`/`nodev` on the state directory.
   - It is more machinery for the small gain in 3.2.
3. **A static per-team user in place of DynamicUser.**
   - It needs sysusers for each team, and uid cleanup when a team is removed.
   - The state directory moves out of `/var/lib/private`.
   - It loses DynamicUser's implied `RemoveIPC=`, which the unit does not set explicitly.
   - **It does not help by itself.** An existing directory owned by nobody is idmapped for a
     static uid too (1.2 step 1), so it needs the chown from option 2 as well.
4. **Wait for an upstream fix.** There is no PR, and the issue has been open since March 2026.
   Not viable for a deploy now.

## Not confirmed

- **The errno on `<host>`.** The host reports EACCES ("Permission denied"). Upstream reports
  EPERM. SELinux is ruled out (1.5), so the difference most likely comes from kernel 6.19's
  checks on opening another task's `uid_map`, but I did not establish this. It does not
  change the cause or the fix.
- **`PrivateUsers=` and the pre-owned directory.** Neither was tested on a real host. Both
  conclusions come from reading the code.
- **The x86_64 / kernel 7.0 "works" comment in #41014.** Its cause is unknown.
- **A fix in v259.11.** I did not check the Fedora f44 systemd patches; Fedora Bugzilla showed
  no entry in this research. The upstream stable tags carry no fix to `userns_acquire()` on
  record.
