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
}
