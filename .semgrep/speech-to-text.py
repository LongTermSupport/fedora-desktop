# Fixture for .semgrep/speech-to-text.yml (Python rules). `ruleid:` marks a line that must
# fire, `ok:` a line that must stay silent.


def installed_by_directory(model_dir):
    # The originating instance in wsi-model-manager: any snapshot at all is "installed".
    # ruleid: model-present-without-weights
    snapshots = model_dir / 'snapshots'
    return snapshots.exists() and any(snapshots.iterdir())


def cpu_percent(proc_root):
    # A word, not a cache path: two /proc stat snapshots taken an interval apart.
    # ok: model-present-without-weights
    snapshots = {"before": proc_root, "after": proc_root}
    return snapshots["after"]


def installed_by_weights(model_dir):
    # ok: model-present-without-weights
    snapshots = model_dir / 'snapshots'
    return snapshots.is_dir() and any((rev / 'model.bin').is_file() for rev in snapshots.iterdir())
