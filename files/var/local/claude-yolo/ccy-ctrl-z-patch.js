#!/usr/bin/env node
/**
 * CCY ctrl+z suspend patch for Claude Code (Ink framework).
 *
 * Ink intercepts ctrl+z BEFORE the keybinding system and calls
 * process.kill(pid, 'SIGSTOP') — an unblockable signal. In a CCY container,
 * this makes Claude unrecoverable (no shell to run `fg`).
 *
 * Supports two Claude Code packaging formats:
 *
 *   1. Legacy cli.js (pre-2.1.x): plain JavaScript file.
 *      Patch appends `&&!process.env.CCY_DISABLE_SUSPEND` to the platform guard.
 *
 *   2. Native binary (2.1.x+): ELF with embedded JS (Node.js SEA).
 *      All binary replacements are SAME-BYTE-LENGTH (a length mismatch is a hard
 *      error, never written) so the ELF layout stays intact. Strategies, in order:
 *
 *        a) PRIMARY — no-op the handleSuspend() method itself. We target the
 *           method, not the guard that calls it, because the *guard condition*
 *           churns between releases (a folded `process.platform!=="win32"`
 *           boolean in older builds; a shared `ano()` predicate call in 2.1.198+:
 *             if(s.name==="z"&&s.ctrl&&ano()){e.handleSuspend();continue}
 *           ) whereas the method is single-purpose (its only job is SIGSTOP), has
 *           no unrelated callers, and is stable. We replace its first statement
 *           with an unconditional `return;`, padded to the same byte length with a
 *           block comment. NB we do NOT flip `ano()`'s body — that predicate is
 *           reused elsewhere for unrelated foreground-only behaviour.
 *
 *        b) LEGACY — pre-2.1.198 bare-variable guard folded from the platform
 *           check:  if(<VAR>){<ref>.handleSuspend();continue}  with `<VAR>=!0`.
 *           Flip the constant `<VAR>=!0` -> `<VAR>=!1` (or invert an unoptimized
 *           `!==` compare to `===`). Absent in newer builds, so it no-matches.
 *
 * The patch is BEST-EFFORT: if all strategies fail, we warn but do NOT fail
 * the build (soft-fail). Ctrl+z may freeze the container in that case.
 * See CLAUDE/ContainerRules.md "Known Fragile Patch" section for manual fix instructions.
 */

'use strict';

const fs = require('fs');
const path = require('path');

const PKG_DIR = process.env.CCY_PKG_DIR || '/usr/local/lib/node_modules/@anthropic-ai/claude-code';
const CLI_JS_PATH = process.env.CCY_CLI_PATH || path.join(PKG_DIR, 'cli.js');
const BINARY_PATH = path.join(PKG_DIR, 'bin', 'claude.exe');
const SUSPEND_GUARD = '&&!process.env.CCY_DISABLE_SUSPEND';

// CCY-07: when the patch soft-fails (target not found), drop a sentinel the
// entrypoint reads at launch so a "ctrl+z freezes the container" outcome is
// diagnosable instead of being a single line buried in the build log. "Sentinel
// absent" means "patched OK". Because this script now also runs during the daily
// in-place Claude update (not only at image build), we clear any stale sentinel
// on success — otherwise a failure from an earlier run would keep warning at
// launch even after a later run patched cleanly.
const STATUS_PATH = process.env.CCY_PATCH_STATUS_PATH || '/opt/claude-yolo/.ctrlz-patch-status';

function writeFailedSentinel() {
    try {
        fs.mkdirSync(path.dirname(STATUS_PATH), { recursive: true });
        fs.writeFileSync(STATUS_PATH, 'failed\n');
    } catch (err) {
        process.stderr.write('CCY PATCH WARNING: could not write ctrl+z status sentinel: ' + err.message + '\n');
    }
}

function clearFailedSentinel() {
    try {
        if (fs.existsSync(STATUS_PATH)) fs.unlinkSync(STATUS_PATH);
    } catch (err) {
        process.stderr.write('CCY PATCH WARNING: could not clear ctrl+z status sentinel: ' + err.message + '\n');
    }
}

const hasCliJs = fs.existsSync(CLI_JS_PATH);
const hasBinary = fs.existsSync(BINARY_PATH);

if (!hasCliJs && !hasBinary) {
    process.stderr.write('CCY PATCH ERROR: Neither cli.js nor bin/claude.exe found in ' + PKG_DIR + '\n');
    process.exit(1);
}

// Clear any stale failure sentinel up front. Every success path below exits
// without rewriting it, so only softFail() re-creates it — meaning a clean run
// (including a re-patch after an in-place Claude update) leaves no sentinel.
clearFailedSentinel();

if (hasCliJs) {
    patchCliJs();
} else {
    patchNativeBinary();
}

// =============================================================================
// Legacy: patch cli.js (text-based replacement, variable length OK)
// =============================================================================
function patchCliJs() {
    let src;
    try {
        src = fs.readFileSync(CLI_JS_PATH, 'utf8');
    } catch (err) {
        process.stderr.write('CCY PATCH ERROR: Cannot read cli.js: ' + err.message + '\n');
        process.exit(1);
    }

    if (src.includes(SUSPEND_GUARD)) {
        process.stdout.write('CCY PATCH: ctrl+z suspend patch already applied - skipping\n');
        process.exit(0);
    }

    // Strategy 1: Known patterns from previous Claude Code versions.
    const knownPatterns = [
        'wT5=process.platform!=="win32"',
        'fG5=process.platform!=="win32"',
    ];

    for (const orig of knownPatterns) {
        if (!src.includes(orig)) continue;
        const count = src.split(orig).length - 1;
        if (count !== 1) {
            process.stderr.write('CCY PATCH WARNING: known pattern "' + orig + '" found ' + count + ' times - skipping\n');
            continue;
        }
        src = src.replace(orig, orig + SUSPEND_GUARD);
        fs.writeFileSync(CLI_JS_PATH, src);
        process.stdout.write('CCY PATCH: ctrl+z suspend patch applied via cli.js (known pattern: ' + JSON.stringify(orig) + ')\n');
        process.exit(0);
    }

    // Strategy 2: Dynamic discovery
    const ctrlZMatch = src.match(/z\.name==="z"&&z\.ctrl&&(\w+)/);
    if (ctrlZMatch) {
        const varName = ctrlZMatch[1];
        const orig = varName + '=process.platform!=="win32"';
        if (src.includes(orig)) {
            const count = src.split(orig).length - 1;
            if (count === 1) {
                src = src.replace(orig, orig + SUSPEND_GUARD);
                fs.writeFileSync(CLI_JS_PATH, src);
                process.stdout.write('CCY PATCH: ctrl+z suspend patch applied via cli.js (dynamic: ' + JSON.stringify(orig) + ')\n');
                process.stdout.write('CCY PATCH INFO: Add "' + orig + '" to knownPatterns to avoid dynamic search next build\n');
                process.exit(0);
            }
        }
    }

    softFail('cli.js');
}

// =============================================================================
// Native binary: patch embedded JS (binary-safe same-length replacement)
// =============================================================================
function patchNativeBinary() {
    let buf;
    try {
        buf = fs.readFileSync(BINARY_PATH);
    } catch (err) {
        process.stderr.write('CCY PATCH ERROR: Cannot read binary: ' + err.message + '\n');
        process.exit(1);
    }

    // Convert to string for pattern discovery. Binary sections produce garbage
    // but ASCII JS patterns survive intact — we only use this for regex matching.
    const src = buf.toString('latin1');

    // Strategy 1 (PRIMARY): no-op the handleSuspend() method itself.
    // The guard *condition* churns between releases (a folded platform boolean in
    // older builds, a shared `ano()` predicate call in 2.1.198+), which is exactly
    // what keeps breaking condition-based patches. The *method* is single-purpose
    // — its only job is to raise SIGSTOP — has no unrelated callers, and is far
    // more stable, so nooping it is the robust target. (This is why we do NOT flip
    // the body of `ano()`: that predicate is reused elsewhere for unrelated
    // foreground-only behaviour.)
    if (tryNoopHandleSuspend(buf, src)) return;

    // Strategy 2 (LEGACY): older native builds gate suspend on a folded platform
    // boolean:  if(<VAR>){<ref>.handleSuspend();continue}  with  <VAR>=!0 .
    // Flip that constant to false. Newer builds have no such variable, so this is
    // a no-match there and we fall through.
    if (tryLegacyPlatformConst(buf, src)) return;

    softFail('native binary (no known handleSuspend method shape or platform-guard variable matched)');
}

// Strategy 1: replace the first statement of the handleSuspend() arrow function
// with an unconditional early `return;`, padded to the SAME byte length so the
// ELF layout is untouched. Everything after the injected return becomes valid but
// unreachable dead code. Returns true if it patched or found an existing patch;
// false if the method shape was not recognised (so the caller tries the next
// strategy).
function tryNoopHandleSuspend(buf, src) {
    const ANCHOR = 'handleSuspend=()=>{';

    // Already patched? Our no-op always begins the body with `return;`.
    if (src.includes(ANCHOR + 'return;')) {
        process.stdout.write('CCY PATCH: ctrl+z patch already applied (handleSuspend no-op) - skipping\n');
        process.exit(0);
    }

    // Known first-statement idioms that immediately follow the method's `{`.
    // We overwrite a WHOLE statement so the rest of the body stays parseable.
    // Add new variants here if a future release changes the opening statement.
    const firstStatements = [
        'if(!this.isRawModeSupported())return;',
    ];

    for (const stmt of firstStatements) {
        const find = ANCHOR + stmt;
        const findBuf = Buffer.from(find, 'latin1');
        const count = bufferCount(buf, findBuf);
        if (count === 0) continue;

        const noop = buildSameLengthNoop(stmt.length);
        if (noop === null) continue; // statement too short to hold `return;`
        const replace = ANCHOR + noop;
        const replaceBuf = Buffer.from(replace, 'latin1');
        if (replaceBuf.length !== findBuf.length) {
            process.stderr.write('CCY PATCH BUG: no-op length mismatch — not writing\n');
            process.exit(1);
        }

        // Patch all occurrences (SEA binaries can duplicate the JS blob).
        bufferReplaceAll(buf, findBuf, replaceBuf);
        fs.writeFileSync(BINARY_PATH, buf);
        process.stdout.write('CCY PATCH: ctrl+z suspend disabled by no-op of handleSuspend() (' + count + ' occurrence(s))\n');
        process.exit(0);
    }

    return false;
}

// Strategy 2: the pre-2.1.198 shape — a bare-variable guard folded from the
// platform check. Flip its boolean constant (or invert the unoptimized compare).
// Returns false when this shape is absent.
function tryLegacyPlatformConst(buf, src) {
    const guardMatch = src.match(/if\((\w{2,8})\)\{[A-Za-z0-9_$.]+\.handleSuspend\(\);continue\}/);
    if (!guardMatch) return false;
    const guardVar = guardMatch[1];
    process.stdout.write('CCY PATCH: found legacy ctrl+z guard variable: ' + guardVar + '\n');

    //   <var>=!0  -> true  (process.platform !== "win32" folded at build time)
    //   <var>=!1  -> false (already patched)
    if (src.includes(guardVar + '=!1')) {
        process.stdout.write('CCY PATCH: ctrl+z suspend patch already applied - skipping\n');
        process.exit(0);
    }

    const findBuf = Buffer.from(guardVar + '=!0', 'latin1');
    const replaceBuf = Buffer.from(guardVar + '=!1', 'latin1');
    const count = bufferCount(buf, findBuf);
    if (count > 0) {
        bufferReplaceAll(buf, findBuf, replaceBuf);
        fs.writeFileSync(BINARY_PATH, buf);
        process.stdout.write('CCY PATCH: ctrl+z suspend patch applied to native binary (' + count + ' occurrence(s), ' + guardVar + '=!0 → ' + guardVar + '=!1)\n');
        process.exit(0);
    }

    // Guard variable found but assignment differs — unoptimized platform compare.
    const altFind = guardVar + '=process.platform!=="win32"';
    if (src.includes(altFind)) {
        // Same-length inversion: !== → === (true only on win32 → false on Linux).
        const altReplace = guardVar + '=process.platform==="win32"';
        if (altFind.length !== altReplace.length) {
            process.stderr.write('CCY PATCH BUG: unoptimized pattern length mismatch\n');
            process.exit(1);
        }
        const altFindBuf = Buffer.from(altFind, 'latin1');
        const altCount = bufferCount(buf, altFindBuf);
        if (altCount > 0) {
            bufferReplaceAll(buf, altFindBuf, Buffer.from(altReplace, 'latin1'));
            fs.writeFileSync(BINARY_PATH, buf);
            process.stdout.write('CCY PATCH: ctrl+z suspend patch applied to native binary (' + altCount + ' occurrence(s), unoptimized !== → ===)\n');
            process.exit(0);
        }
    }

    // Legacy guard var present but neither assignment shape matched — let the
    // caller fall through to softFail rather than claim success.
    return false;
}

// Build a same-length no-op statement: `return;` followed by padding to `len`
// bytes. Uses a block comment when there is room for one (>= 4 pad bytes), else
// trailing spaces. Returns null if `len` cannot even hold `return;`.
function buildSameLengthNoop(len) {
    const RET = 'return;';
    if (len < RET.length) return null;
    const pad = len - RET.length;
    if (pad === 0) return RET;
    if (pad >= 4) return RET + '/*' + '.'.repeat(pad - 4) + '*/';
    return RET + ' '.repeat(pad);
}

// =============================================================================
// Helpers
// =============================================================================
function bufferCount(buf, pattern) {
    let count = 0;
    let pos = 0;
    while (true) {
        pos = buf.indexOf(pattern, pos);
        if (pos === -1) break;
        count++;
        pos += pattern.length;
    }
    return count;
}

function bufferReplaceAll(buf, find, replace) {
    let pos = 0;
    while (true) {
        pos = buf.indexOf(find, pos);
        if (pos === -1) break;
        replace.copy(buf, pos);
        pos += replace.length;
    }
}

function softFail(context) {
    writeFailedSentinel();
    process.stderr.write('CCY PATCH WARNING: ctrl+z patch target not found in ' + context + ' - skipping (Claude Code internals changed)\n');
    process.stderr.write('CCY PATCH INFO: ctrl+z may freeze the container. The CCY_DISABLE_SUSPEND env var will have no effect.\n');
    process.stderr.write('CCY PATCH INFO: To debug: grep -ao ".\\{0,5\\}handleSuspend.\\{0,100\\}" <binary-path>\n');
    process.exit(0);
}
