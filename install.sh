#!/bin/sh
# Installs the latest release of rein, or the one REIN_VERSION names, into
# ~/.local/bin or REIN_INSTALL_DIR: the archive for this machine, checked
# against the release's checksum file. No package manager needed.
#
#   curl -fsSL https://raw.githubusercontent.com/Danidani23/agents-rein/main/install.sh | sh
set -eu

repository="Danidani23/agents-rein"
version="${REIN_VERSION:-latest}"
target_dir="${REIN_INSTALL_DIR:-$HOME/.local/bin}"

os="$(uname -s | tr '[:upper:]' '[:lower:]')"
case "$os" in
  darwin|linux) ;;
  *) echo "rein has no build for $os; on Windows use install.ps1" >&2; exit 1 ;;
esac
arch="$(uname -m)"
case "$arch" in
  x86_64|amd64) arch="amd64" ;;
  arm64|aarch64) arch="arm64" ;;
  *) echo "rein has no build for $arch" >&2; exit 1 ;;
esac

if [ "$version" = "latest" ]; then
  version="$(curl -fsSL "https://api.github.com/repos/$repository/releases/latest" | sed -n 's/.*"tag_name": *"\([^"]*\)".*/\1/p' | head -1)"
  [ -n "$version" ] || { echo "could not read the latest release of $repository" >&2; exit 1; }
fi
number="${version#v}"
archive="rein_${number}_${os}_${arch}.tar.gz"
checksums="rein_${number}_checksums.txt"
base="https://github.com/$repository/releases/download/$version"

scratch="$(mktemp -d)"
trap 'rm -rf "$scratch"' EXIT
echo "downloading rein $version for $os/$arch"
curl -fsSL -o "$scratch/$archive" "$base/$archive"
curl -fsSL -o "$scratch/$checksums" "$base/$checksums"

expected="$(grep " $archive\$" "$scratch/$checksums" | awk '{print $1}')"
if command -v sha256sum >/dev/null 2>&1; then
  actual="$(sha256sum "$scratch/$archive" | awk '{print $1}')"
else
  actual="$(shasum -a 256 "$scratch/$archive" | awk '{print $1}')"
fi
[ -n "$expected" ] || { echo "the checksum file names no $archive; not installing" >&2; exit 1; }
[ "$expected" = "$actual" ] || { echo "checksum mismatch for $archive; not installing" >&2; exit 1; }

tar -xzf "$scratch/$archive" -C "$scratch" rein
mkdir -p "$target_dir"
install -m 0755 "$scratch/rein" "$target_dir/rein"
echo "installed rein $version to $target_dir/rein"
case ":$PATH:" in
  *":$target_dir:"*) ;;
  *) echo "add $target_dir to your PATH, for example: export PATH=\"$target_dir:\$PATH\"" ;;
esac
