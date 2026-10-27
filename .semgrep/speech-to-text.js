// Fixture for .semgrep/speech-to-text.yml (JavaScript rules). `ruleid:` marks a line that
// must fire, `ok:` a line that must stay silent.

const WHISPER_MODELS = [
    // ok: dropdown-label-carries-explanation
    ['tiny', 'Systran/faster-whisper-tiny', 'Tiny'],
    // ruleid: dropdown-label-carries-explanation
    ['distil', 'distil-whisper/distil-large-v3.5-ct2', 'Distil Large v3.5 English (~1.5GB)'],
];

class Fixture {
    // The originating instance: any child of snapshots/ counted as installed.
    _isModelInstalledByDirectory(repoId) {
        // ruleid: model-present-without-weights
        const dir = Gio.File.new_for_path(`${cache}/models--${repoId}/snapshots`);
        return dir.enumerate_children('standard::name', 0, null).next_file(null) !== null;
    }

    _isModelInstalledByWeights(repoId) {
        // ok: model-present-without-weights
        const dir = Gio.File.new_for_path(`${cache}/models--${repoId}/snapshots`);
        const info = dir.enumerate_children('standard::name', 0, null).next_file(null);
        return info !== null && dir.get_child(info.get_name()).get_child('model.bin').query_exists(null);
    }

    fillPage(group, settings) {
        this._addComboRow(group, settings, 'streaming-startup-mode',
            'Startup mode', 'Standard: load the model at each Insert, then record',
            ['standard', 'server'],
            // ruleid: dropdown-label-carries-explanation
            ['Standard — load then start (~3-6s)',
                // ok: dropdown-label-carries-explanation
                'Server']);

        this._addComboRow(group, settings, 'claude-model',
            'Claude model', 'Sonnet balances speed and quality',
            ['sonnet'],
            // ruleid: dropdown-label-carries-explanation
            ['Sonnet, balanced']);
    }

    buildList(currentModel) {
        // ok: dropdown-label-carries-explanation
        const labels = ['Auto'];
        for (const [id, , label] of WHISPER_MODELS) {
            if (id === currentModel)
                // ruleid: dropdown-label-carries-explanation
                labels.push(`${label} — not installed`);
            else
                // ok: dropdown-label-carries-explanation
                labels.push(label);
        }
        // ruleid: dropdown-label-carries-explanation
        labels.push('An option name that runs on too long');
        return labels;
    }

    // The originating instance: the recorder's group killed, its recording left in /dev/shm.
    _discardRecordingLeavingAudio(pid, launch) {
        // ruleid: recorder-killed-leaving-its-files
        const {signal, group} = discardSignal(launch);
        this._signalProcess(pid, signal, group);
    }

    _killLeavingAudio(pid) {
        // ruleid: recorder-killed-leaving-its-files
        this._signalProcess(pid, 'KILL', true);
        // ruleid: recorder-killed-leaving-its-files
        GLib.spawn_async(null, ['kill', '-9', String(pid)], null, 0, null);
    }

    _killBySignalName(proc, pid) {
        // ruleid: recorder-killed-leaving-its-files
        GLib.spawn_async(null, ['kill', '-s', 'KILL', String(pid)], null, 0, null);
        // ruleid: recorder-killed-leaving-its-files
        proc.send_signal(9);
        // ruleid: recorder-killed-leaving-its-files
        proc.force_exit();
    }

    _discardRecording(pid, launch) {
        // ok: recorder-killed-leaving-its-files
        const {signal, group} = discardSignal(launch);
        this._send(killArgv(pid, signal, group));
        return () => discardLeftovers(launch, this._who).forEach(this._remove);
    }

    _stop(pid) {
        // ok: recorder-killed-leaving-its-files
        this._signalProcess(pid, 'TERM');
    }
}

// ok: recorder-killed-leaving-its-files
const SIGNALS = ['TERM', 'KILL'];

export function discardLeavingAudio(pid, launch, send) {
    // ruleid: recorder-killed-leaving-its-files
    const {signal, group} = discardSignal(launch);
    send(killArgv(pid, signal, group));
}

export function discardRecorder(pid, launch, {who, send, remove}) {
    const leftovers = discardLeftovers(launch, who);
    // ok: recorder-killed-leaving-its-files
    const {signal, group} = discardSignal(launch);
    send(killArgv(pid, signal, group));
    return () => leftovers.forEach(remove);
}

// ruleid: recorder-killed-leaving-its-files
const killNow = pid => run(['kill', '-SIGKILL', String(pid)]);

const discardByArrow = (pid, launch, who) => {
    // ok: recorder-killed-leaving-its-files
    send(killArgv(pid, discardSignal(launch).signal, true));
    return () => discardLeftovers(launch, who).forEach(remove);
};

const discardByExpression = function (pid, launch, who) {
    // ok: recorder-killed-leaving-its-files
    send(killArgv(pid, discardSignal(launch).signal, true));
    return () => discardLeftovers(launch, who).forEach(remove);
};

const killQuietly = pid => {
    // ruleid: recorder-killed-leaving-its-files
    signalProcess(pid, 'SIGKILL');
};

// ok: recorder-killed-leaving-its-files
test('Escape kills a batch recorder', () => discardSignal({serverMode: false}));

// ruleid: pw-record-stdout-not-raw
const captureArgv = ['pw-record', '--rate', '16000', '--format', 's16', '-'];

// ok: pw-record-stdout-not-raw
const rawCaptureArgv = ['pw-record', '--raw', '--rate', '16000', '--format', 's16', '-'];

// ok: pw-record-stdout-not-raw
const fileCaptureArgv = ['pw-record', '--rate', '44100', '/dev/shm/recording.wav'];

// ruleid: pw-record-stdout-not-raw
const shellCapture = `pw-record --rate 16000 - | sox -t raw -r 16000 - out.wav`;
