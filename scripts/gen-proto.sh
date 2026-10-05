#!/usr/bin/env bash
# Генерация кода из proto/ (ADR-0001). Запускать из корня: make proto
#   1. Go: сообщения + gRPC + gateway (core) → backend/gen
#   2. Python: только ML-контракт → ml/gen (нужен uv)
#   3. OpenAPI v3 для core → frontend/src/api/openapi.yaml → schema.d.ts (TS-типы нужен npx)
# Шаги 2 и 3 пропускаются с предупреждением, если нет uv / npx: их коммитят те, у кого они есть.
set -euo pipefail
cd "$(dirname "$0")/.."

export PATH="$(go env GOPATH)/bin:$PATH"
for tool in protoc protoc-gen-go protoc-gen-go-grpc protoc-gen-grpc-gateway protoc-gen-openapi; do
  command -v "$tool" >/dev/null || { echo "нет $tool — запустите make proto-tools" >&2; exit 1; }
done

CORE=$(find proto/alfa/core -name '*.proto' | sort)
ML=$(find proto/alfa/ml -name '*.proto' | sort)

# --- 1. Go -------------------------------------------------------------------
rm -rf backend/gen/alfa
mkdir -p backend/gen
protoc -I proto -I proto/third_party \
  --go_out=backend/gen --go_opt=paths=source_relative \
  --go-grpc_out=backend/gen --go-grpc_opt=paths=source_relative \
  $CORE $ML
# gateway — только для публичного API (в ml/v1 нет http-аннотаций)
protoc -I proto -I proto/third_party \
  --grpc-gateway_out=backend/gen --grpc-gateway_opt=paths=source_relative \
  $CORE
echo "go:      backend/gen"

# --- 2. Python ---------------------------------------------------------------
if command -v uv >/dev/null && [ -f ml/pyproject.toml ]; then
  rm -rf ml/gen/alfa
  mkdir -p ml/gen
  uv run --project ml python -m grpc_tools.protoc -I proto \
    --python_out=ml/gen --pyi_out=ml/gen --grpc_python_out=ml/gen \
    $ML
  echo "python:  ml/gen"
else
  echo "WARNING: python пропущен (нужны uv и ml/pyproject.toml): ml/gen не обновлён" >&2
fi

# --- 3. OpenAPI + TypeScript -------------------------------------------------
mkdir -p frontend/src/api
protoc -I proto -I proto/third_party \
  --openapi_out=frontend/src/api --openapi_opt=enum_type=string,default_response=false \
  $CORE
echo "openapi: frontend/src/api/openapi.yaml"
if command -v npx >/dev/null && [ -f frontend/package.json ]; then
  npx --prefix frontend openapi-typescript frontend/src/api/openapi.yaml -o frontend/src/api/schema.d.ts
  echo "ts:      frontend/src/api/schema.d.ts"
else
  echo "WARNING: TypeScript пропущен (нужны node и frontend/package.json): schema.d.ts не обновлён" >&2
fi
