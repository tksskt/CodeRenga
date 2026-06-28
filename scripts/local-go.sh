#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
LOCAL="$ROOT/.local"
GO_VERSION="${GO_VERSION:-1.26.4}"

export PATH="$LOCAL/go/bin:$PATH"
export GOMODCACHE="$LOCAL/cache/go-mod"
export GOCACHE="$LOCAL/cache/go-build"
export GOPATH="$LOCAL/go-path"
export GOBIN="$LOCAL/bin"
export GOTELEMETRY=off

mkdir -p "$LOCAL/cache/downloads" "$GOMODCACHE" "$GOCACHE" "$GOPATH" "$GOBIN"

detect_go_platform() {
  local os arch
  os="$(uname -s)"
  arch="$(uname -m)"

  case "$os" in
    Darwin) os="darwin" ;;
    Linux) os="linux" ;;
    *) echo "unsupported OS: $os" >&2; return 1 ;;
  esac

  case "$arch" in
    arm64|aarch64) arch="arm64" ;;
    x86_64|amd64) arch="amd64" ;;
    *) echo "unsupported arch: $arch" >&2; return 1 ;;
  esac

  printf "%s-%s" "$os" "$arch"
}

ensure_go() {
  local go_bin="$LOCAL/go/bin/go"
  if [ -x "$go_bin" ]; then
    return
  fi

  local platform archive url
  platform="$(detect_go_platform)"
  archive="$LOCAL/cache/downloads/go${GO_VERSION}.${platform}.tar.gz"
  url="https://go.dev/dl/go${GO_VERSION}.${platform}.tar.gz"

  echo "Downloading Go ${GO_VERSION} for ${platform} into .local/"
  curl -fL --retry 3 --output "$archive" "$url"

  rm -rf "$LOCAL/go"
  tar -C "$LOCAL" -xzf "$archive"
}

run_go() {
  ensure_go
  cd "$ROOT"
  go "$@"
}

case "${1:-}" in
  setup)
    ensure_go
    cd "$ROOT"
    go version
    go mod download
    ;;
  fmt)
    ensure_go
    cd "$ROOT"
    gofmt -w cmd internal
    ;;
  lint)
    run_go vet ./...
    ;;
  test)
    run_go test ./...
    ;;
  build)
    ensure_go
    cd "$ROOT"
    mkdir -p "$GOBIN"
    case "$(uname -s)" in
      MINGW*|MSYS*|CYGWIN*) output="$GOBIN/coderenga.exe" ;;
      *) output="$GOBIN/coderenga" ;;
    esac
    go build -o "$output" ./cmd/coderenga
    ;;
  env)
    cat <<EOF
export PATH="$LOCAL/go/bin:\$PATH"
export GOMODCACHE="$GOMODCACHE"
export GOCACHE="$GOCACHE"
export GOPATH="$GOPATH"
export GOBIN="$GOBIN"
export GOTELEMETRY=off
EOF
    ;;
  *)
    echo "usage: $0 setup|fmt|lint|test|build|env" >&2
    exit 2
    ;;
esac
