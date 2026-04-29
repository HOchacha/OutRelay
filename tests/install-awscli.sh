#!/usr/bin/env bash
# tests/install-awscli.sh
#
# Installs AWS CLI v2 from the official bundle. Idempotent: skips if
# already present. Requires sudo for the system-wide install step.

set -euo pipefail

if command -v aws >/dev/null 2>&1; then
  echo "aws already installed: $(aws --version)"
  exit 0
fi

arch=$(uname -m)
case "$arch" in
  x86_64)  pkg="awscli-exe-linux-x86_64.zip" ;;
  aarch64) pkg="awscli-exe-linux-aarch64.zip" ;;
  *) echo "unsupported arch: $arch" >&2; exit 1 ;;
esac

tmpdir=$(mktemp -d)
trap 'rm -rf "$tmpdir"' EXIT

curl -fsSL "https://awscli.amazonaws.com/$pkg" -o "$tmpdir/awscliv2.zip"

if ! command -v unzip >/dev/null 2>&1; then
  sudo apt-get update -qq
  sudo apt-get install -y unzip
fi

unzip -q "$tmpdir/awscliv2.zip" -d "$tmpdir"
sudo "$tmpdir/aws/install"

aws --version
