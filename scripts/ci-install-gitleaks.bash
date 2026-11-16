#!/usr/bin/env bash
# ci-install-gitleaks.bash VERSION — install gitleaks at VERSION (the GITLEAKS pin of
# .qa-versions) into /usr/local/bin on a GitHub Actions runner, for both jobs of
# .github/workflows/qa.yml that run scripts/qa-gitleaks.bash. The archive is checked against
# the release's own checksums file before it is unpacked. The host gets gitleaks from
# playbooks/imports/play-python.yml and the ccy image from .claude/ccy/Dockerfile.
set -euo pipefail

version="${1:?usage: ci-install-gitleaks.bash VERSION}"
case "$(uname -m)" in
    x86_64) arch=x64 ;;
    aarch64) arch=arm64 ;;
    *) echo "ci-install-gitleaks: no gitleaks build for $(uname -m)" >&2; exit 1 ;;
esac
archive="gitleaks_${version}_linux_${arch}.tar.gz"
release="https://github.com/gitleaks/gitleaks/releases/download/v${version}"
dir="$(mktemp -d)"
curl -fsSL -o "${dir}/${archive}" "${release}/${archive}"
curl -fsSL -o "${dir}/checksums.txt" "${release}/gitleaks_${version}_checksums.txt"
(cd "${dir}" && grep " ${archive}\$" checksums.txt | sha256sum -c -)
tar -xzf "${dir}/${archive}" -C "${dir}" gitleaks
sudo install -m 0755 "${dir}/gitleaks" /usr/local/bin/gitleaks
gitleaks version
