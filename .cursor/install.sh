#!/usr/bin/env bash
# Idempotent Cloud Agent setup for pigo.
# Installs the Go 1.27 toolchain and golangci-lint, clones a read-only behaviour
# reference to ~/deps/pi (for GitHub issue work; never imported), then refreshes
# module deps and builds.
set -euo pipefail

GO_VERSION="1.27.0"
GO_TARBALL="go${GO_VERSION}.linux-amd64.tar.gz"
GO_INSTALL_DIR="/usr/local/go127"

# --- Go 1.27 toolchain (the default image ships an older, EOL Go) ---
if ! go version 2>/dev/null | grep -q "go${GO_VERSION} "; then
  echo "Installing Go ${GO_VERSION}..."
  tmp="$(mktemp -d)"
  curl -fsSL --retry 5 --retry-delay 2 --retry-all-errors \
    -o "${tmp}/${GO_TARBALL}" "https://go.dev/dl/${GO_TARBALL}"
  sudo rm -rf "${GO_INSTALL_DIR}"
  sudo mkdir -p "${GO_INSTALL_DIR}"
  sudo tar -C "${GO_INSTALL_DIR}" --strip-components=1 -xzf "${tmp}/${GO_TARBALL}"
  # /usr/local/bin precedes /usr/bin on PATH, so these shadow the system Go.
  sudo ln -sf "${GO_INSTALL_DIR}/bin/go" /usr/local/bin/go
  sudo ln -sf "${GO_INSTALL_DIR}/bin/gofmt" /usr/local/bin/gofmt
  rm -rf "${tmp}"
fi
echo "Using $(go version)"

# --- golangci-lint ---
if ! command -v golangci-lint >/dev/null 2>&1; then
  echo "Installing golangci-lint..."
  curl -fsSL https://raw.githubusercontent.com/golangci/golangci-lint/HEAD/install.sh \
    | sudo sh -s -- -b /usr/local/bin
fi
echo "Using $(golangci-lint version 2>/dev/null || echo 'golangci-lint (version unavailable)')"

# --- behaviour reference (read-only, never imported; used when working issues) ---
REF_DIR="${HOME}/deps/pi"
if [ ! -d "${REF_DIR}/.git" ]; then
  echo "Cloning behaviour reference into ${REF_DIR}..."
  mkdir -p "$(dirname "${REF_DIR}")"
  git clone --depth 1 https://github.com/earendil-works/pi.git "${REF_DIR}" \
    || echo "warning: could not clone behaviour reference (continuing without it)"
else
  echo "behaviour reference already present at ${REF_DIR}"
fi

# --- project dependencies & build ---
go mod download
go build ./...
go vet ./...

echo "pigo environment ready."
