# Минимальный Makefile: только то, что уже есть в репозитории.
# Остальные цели (up, data, train, …) добавляются вместе с соответствующими модулями.
.DEFAULT_GOAL := help
.PHONY: help proto-tools proto build test

help:
	@grep -E '^[a-z-]+:.*##' $(MAKEFILE_LIST) | awk -F':.*## ' '{printf "  %-12s %s\n", $$1, $$2}'

proto-tools: ## Установить protoc-плагины закреплённых версий
	./scripts/proto-tools.sh

proto: ## Сгенерировать Go, Python, OpenAPI и TS из proto/
	./scripts/gen-proto.sh

build: ## Собрать backend
	cd backend && go build ./...

test: ## Тесты backend
	cd backend && go test ./...
