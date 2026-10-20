#!/usr/bin/env bash
# Idempotent Cloud Agent setup for pigo.
# Installs the Go 1.27 toolchain and golangci-lint, checks out the pinned
# behaviour reference (earendil-works/pi, read-only, never imported) under
# ~/deps/pi, then refreshes module deps and builds.
set -euo pipefail

GO_VERSION="1.27.0"
# Frozen for the first pigo release. Do not bump without a decision in an issue.
PI_REF_TAG="v1.3.0"
PI_REF_DIR="${HOME}/deps/pi"
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

# --- behaviour reference: earendil-works/pi pinned to ${PI_REF_TAG} ---
if [ ! -d "${PI_REF_DIR}/.git" ]; then
  echo "Cloning pi ${PI_REF_TAG} into ${PI_REF_DIR}..."
  mkdir -p "$(dirname "${PI_REF_DIR}")"
  git -c advice.detachedHead=false clone --quiet --depth 1 --branch "${PI_REF_TAG}" \
    https://github.com/earendil-works/pi.git "${PI_REF_DIR}" \
    || echo "warning: could not clone pi reference (continuing without it)"
elif [ "$(git -C "${PI_REF_DIR}" describe --tags --exact-match 2>/dev/null)" != "${PI_REF_TAG}" ]; then
  echo "Resetting pi reference to ${PI_REF_TAG}..."
  { git -C "${PI_REF_DIR}" fetch --quiet --depth 1 origin "refs/tags/${PI_REF_TAG}:refs/tags/${PI_REF_TAG}" \
      && git -C "${PI_REF_DIR}" checkout --quiet "${PI_REF_TAG}"; } \
    || echo "warning: could not reset pi reference (continuing)"
fi
echo "pi reference: $(git -C "${PI_REF_DIR}" describe --tags --exact-match 2>/dev/null || echo 'unavailable') at ${PI_REF_DIR}"

# --- project dependencies & build ---
go mod download
go build ./...
go vet ./...

echo "pigo environment ready."
