#!/usr/bin/env bash
# Ставит Go-плагины protoc закреплённых версий в $(go env GOPATH)/bin и проверяет protoc.
# Сам protoc: brew install protobuf | apt install protobuf-compiler | GitHub Releases protobuf.
set -euo pipefail
cd "$(dirname "$0")/.."
# shellcheck source=proto-tools.env
source scripts/proto-tools.env

command -v go >/dev/null || { echo "go не найден: https://go.dev/dl/ (нужен 1.24+)" >&2; exit 1; }
command -v protoc >/dev/null || { echo "protoc не найден: brew install protobuf (или apt install protobuf-compiler)" >&2; exit 1; }

have="$(protoc --version | awk '{print $2}')"
if [ "$have" != "$PROTOC_VERSION" ]; then
  echo "WARNING: protoc $have, а в proto-tools.env закреплён $PROTOC_VERSION — в шапках *.pb.go появится лишний дифф" >&2
fi

go install "google.golang.org/protobuf/cmd/protoc-gen-go@${PROTOC_GEN_GO_VERSION}"
go install "google.golang.org/grpc/cmd/protoc-gen-go-grpc@${PROTOC_GEN_GO_GRPC_VERSION}"
go install "github.com/grpc-ecosystem/grpc-gateway/v2/protoc-gen-grpc-gateway@${GRPC_GATEWAY_VERSION}"
go install "github.com/google/gnostic/cmd/protoc-gen-openapi@${GNOSTIC_VERSION}"
echo "OK: плагины установлены в $(go env GOPATH)/bin"
