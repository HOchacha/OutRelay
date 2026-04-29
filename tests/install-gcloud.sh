#!/usr/bin/env bash
# tests/install-gcloud.sh
#
# Installs Google Cloud SDK from the official apt repository on
# Debian/Ubuntu. Idempotent: skips if already present. Requires sudo
# for repo registration and package install.

set -euo pipefail

if command -v gcloud >/dev/null 2>&1; then
  echo "gcloud already installed: $(gcloud --version | head -1)"
  exit 0
fi

sudo apt-get install -y apt-transport-https ca-certificates gnupg curl

keyring=/usr/share/keyrings/cloud.google.gpg
if [[ ! -f "$keyring" ]]; then
  curl -fsSL https://packages.cloud.google.com/apt/doc/apt-key.gpg \
    | sudo gpg --dearmor -o "$keyring"
fi

list=/etc/apt/sources.list.d/google-cloud-sdk.list
if [[ ! -f "$list" ]]; then
  echo "deb [signed-by=$keyring] https://packages.cloud.google.com/apt cloud-sdk main" \
    | sudo tee "$list" >/dev/null
fi

sudo apt-get update -qq
sudo apt-get install -y google-cloud-cli

gcloud --version | head -1
