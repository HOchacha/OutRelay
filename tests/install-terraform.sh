#!/usr/bin/env bash
# tests/install-terraform.sh
#
# Installs Terraform from the official HashiCorp apt repository on
# Debian/Ubuntu. Idempotent: skips if already present. Requires sudo
# for repo registration and package install.

set -euo pipefail

if command -v terraform >/dev/null 2>&1; then
  echo "terraform already installed: $(terraform version | head -1)"
  exit 0
fi

sudo apt-get install -y apt-transport-https ca-certificates gnupg curl lsb-release

keyring=/usr/share/keyrings/hashicorp-archive-keyring.gpg
if [[ ! -f "$keyring" ]]; then
  curl -fsSL https://apt.releases.hashicorp.com/gpg \
    | sudo gpg --dearmor -o "$keyring"
fi

list=/etc/apt/sources.list.d/hashicorp.list
if [[ ! -f "$list" ]]; then
  codename=$(lsb_release -cs)
  echo "deb [signed-by=$keyring] https://apt.releases.hashicorp.com $codename main" \
    | sudo tee "$list" >/dev/null
fi

sudo apt-get update -qq
sudo apt-get install -y terraform

terraform version | head -1
