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
 *      The platform check is optimized to a boolean constant at build time
 *      (e.g., `xQ4=!0`). Patch flips it to `=!1` (false) — same byte length,
 *      safe for binary replacement.
 *
 * Both modes use two strategies in order:
 *   1. Known patterns: exact strings confirmed in previous Claude Code versions.
 *   2. Dynamic discovery: regex search near the handleSuspend() call site.
 *
 * The patch is BEST-EFFORT: if both strategies fail, we warn but do NOT fail
 * the build (soft-fail). Ctrl+z may freeze the container in that case.
 * See CLAUDE.md "KNOWN FRAGILE PATCH" section for manual fix instructions.
 */

'use strict';

const fs = require('fs');
const path = require('path');

const PKG_DIR = process.env.CCY_PKG_DIR || '/usr/local/lib/node_modules/@anthropic-ai/claude-code';
const CLI_JS_PATH = process.env.CCY_CLI_PATH || path.join(PKG_DIR, 'cli.js');
const BINARY_PATH = path.join(PKG_DIR, 'bin', 'claude.exe');
const SUSPEND_GUARD = '&&!process.env.CCY_DISABLE_SUSPEND';

const hasCliJs = fs.existsSync(CLI_JS_PATH);
const hasBinary = fs.existsSync(BINARY_PATH);

if (!hasCliJs && !hasBinary) {
    process.stderr.write('CCY PATCH ERROR: Neither cli.js nor bin/claude.exe found in ' + PKG_DIR + '\n');
    process.exit(1);
}

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

    // Find the ctrl+z guard variable from the handleSuspend call site:
    //   if(<VAR>){<ref>.handleSuspend();continue}
    const guardMatch = src.match(/if\((\w{2,8})\)\{[A-Za-z0-9_$.]+\.handleSuspend\(\);continue\}/);
    if (!guardMatch) {
        softFail('native binary (cannot find handleSuspend guard pattern)');
        return;
    }
    const guardVar = guardMatch[1];
    process.stdout.write('CCY PATCH: found ctrl+z guard variable: ' + guardVar + '\n');

    // In native binaries, the platform check is optimized to a boolean constant:
    //   <var>=!0  means true  (process.platform !== "win32" was true at build time)
    //   <var>=!1  means false (already patched)
    const patchedPattern = guardVar + '=!1';
    if (src.includes(patchedPattern)) {
        process.stdout.write('CCY PATCH: ctrl+z suspend patch already applied - skipping\n');
        process.exit(0);
    }

    const findStr = guardVar + '=!0';
    const replaceStr = guardVar + '=!1';
    const findBuf = Buffer.from(findStr, 'latin1');
    const replaceBuf = Buffer.from(replaceStr, 'latin1');

    if (findBuf.length !== replaceBuf.length) {
        process.stderr.write('CCY PATCH BUG: replacement length mismatch — this should never happen\n');
        process.exit(1);
    }

    const count = bufferCount(buf, findBuf);
    if (count === 0) {
        // Guard variable found but its assignment pattern differs — might use
        // process.platform!=="win32" in unoptimized builds
        const altFind = guardVar + '=process.platform!=="win32"';
        if (src.includes(altFind)) {
            process.stdout.write('CCY PATCH: native binary has unoptimized platform check — unexpected but patchable\n');
            // For unoptimized native binary, we need same-length replacement.
            // Replace !== with === (inverts the condition: true on win32 only → false on Linux)
            const altReplace = guardVar + '=process.platform==="win32"';
            if (altFind.length !== altReplace.length) {
                process.stderr.write('CCY PATCH BUG: unoptimized pattern length mismatch\n');
                process.exit(1);
            }
            const altFindBuf = Buffer.from(altFind, 'latin1');
            const altReplaceBuf = Buffer.from(altReplace, 'latin1');
            const altCount = bufferCount(buf, altFindBuf);
            if (altCount > 0) {
                bufferReplaceAll(buf, altFindBuf, altReplaceBuf);
                fs.writeFileSync(BINARY_PATH, buf);
                process.stdout.write('CCY PATCH: ctrl+z suspend patch applied to native binary (' + altCount + ' occurrence(s), unoptimized !== → ===)\n');
                process.exit(0);
            }
        }
        softFail('native binary (guard var ' + guardVar + ' found but no =!0 assignment)');
        return;
    }

    // Patch all occurrences (SEA binaries often duplicate the JS blob)
    bufferReplaceAll(buf, findBuf, replaceBuf);
    fs.writeFileSync(BINARY_PATH, buf);
    process.stdout.write('CCY PATCH: ctrl+z suspend patch applied to native binary (' + count + ' occurrence(s), ' + findStr + ' → ' + replaceStr + ')\n');
    process.exit(0);
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
    process.stderr.write('CCY PATCH WARNING: ctrl+z patch target not found in ' + context + ' - skipping (Claude Code internals changed)\n');
    process.stderr.write('CCY PATCH INFO: ctrl+z may freeze the container. The CCY_DISABLE_SUSPEND env var will have no effect.\n');
    process.stderr.write('CCY PATCH INFO: To debug: grep -ao ".\\{0,5\\}handleSuspend.\\{0,100\\}" <binary-path>\n');
    process.exit(0);
}
