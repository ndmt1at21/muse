.DEFAULT_GOAL := help

# Connection strings for running services against the docker-compose datastores.
PG_DSN    ?= postgres://muse:muse@localhost:5432/muse?sslmode=disable
MYSQL_DSN ?= muse:muse@tcp(localhost:3306)/muse?parseTime=true&multiStatements=true
REDIS     ?= localhost:6379

.PHONY: help generate build test test-race vet embed up up-mysql down logs \
        migrate migrate-mysql run-core run-core-mysql run-consumer run-admin \
        smoke-rest e2e e2e-shapes e2e-rewards e2e-fulfillment e2e-identity e2e-wallet e2e-hardening e2e-integration seed \
        docs docs-build

help: ## List targets
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | \
	  awk 'BEGIN{FS=":.*?## "}{printf "  \033[36m%-16s\033[0m %s\n", $$1, $$2}'

generate: ## Regenerate protobuf Go code + OpenAPI spec (buf)
	buf lint
	buf generate
	# Wrap the OpenAPI responses in the runtime envelope { code, message, trace_id, data }.
	jq -f core/api/envelope.jq core/api/openapi.swagger.json > core/api/openapi.swagger.json.tmp \
		&& mv core/api/openapi.swagger.json.tmp core/api/openapi.swagger.json

build: ## Build all packages in both modules
	go build ./...
	cd gamekit && go build ./...

test: ## Run unit tests (gamekit SDK runs with no infra — Mode A)
	cd gamekit && go test ./...
	go test ./...

test-race: ## Run gamekit tests with the race detector (stock-race coverage)
	cd gamekit && go test -race ./...

test-integration: ## Run adapter integration tests (real pg+mysql+redis via testcontainers; needs Docker)
	go test -tags integration ./adapters/...

vet: ## go vet both modules
	go vet ./...
	cd gamekit && go vet ./...

embed: ## Run the Mode-A embed example (pure SDK, no DB/Redis)
	go run ./examples/embed

up: ## Start datastores + services (Postgres path) via docker compose
	docker compose -f deploy/docker-compose.yml up -d --build

up-data: ## Start only the datastores (pg + mysql + dragonfly)
	docker compose -f deploy/docker-compose.yml up -d postgres mysql dragonfly

down: ## Stop and remove the stack
	docker compose -f deploy/docker-compose.yml down -v

logs: ## Tail service logs
	docker compose -f deploy/docker-compose.yml logs -f core bff-consumer bff-admin

migrate: ## Apply migrations to Postgres
	DB_ENGINE=postgres DB_DSN="$(PG_DSN)" go run ./core/cmd/core migrate

migrate-mysql: ## Apply migrations to MySQL
	DB_ENGINE=mysql DB_DSN="$(MYSQL_DSN)" go run ./core/cmd/core migrate

run-core: ## Run Core locally against Postgres (gRPC :9090 + REST :8090)
	DB_ENGINE=postgres DB_DSN="$(PG_DSN)" REDIS_ADDR="$(REDIS)" go run ./core/cmd/core

run-core-mysql: ## Run Core locally against MySQL
	DB_ENGINE=mysql DB_DSN="$(MYSQL_DSN)" REDIS_ADDR="$(REDIS)" go run ./core/cmd/core

run-consumer: ## Run the reference consumer BFF locally (examples/)
	go run ./examples/bff-consumer/cmd/bff-consumer

run-admin: ## Run the reference admin BFF locally (examples/)
	go run ./examples/bff-admin/cmd/bff-admin

smoke-rest: ## Smoke-test Core's REST gateway directly (no BFF; expects Core running)
	./deploy/smoke_rest.sh

e2e: ## Run the scripted end-to-end spin-wheel flow (expects services up)
	./deploy/e2e.sh

e2e-shapes: ## Run the egg-catcher + gift-catcher e2e flow (expects services up)
	./deploy/e2e_shapes.sh

e2e-rewards: ## Run the reward-system e2e flow: caps, codes, claim/fulfill/revoke (expects services up)
	./deploy/e2e_rewards.sh

e2e-fulfillment: ## Run the fulfillment e2e: outbox, dispatcher, dead-letter/retry, n8n callback (expects services up)
	./deploy/e2e_fulfillment.sh

e2e-identity: ## Run the identity e2e: one identity across tenants, isolated players, OTP login, JWT (expects services up)
	./deploy/e2e_identity.sh

e2e-wallet: ## Run the wallet e2e: lucky_item credit, balance/ledger, milestone redeem (expects services up)
	./deploy/e2e_wallet.sh

e2e-hardening: ## Run the BFF-hardening e2e: admin RBAC 403/200, read-model cache, gameplay rate-limit 429 (expects services up)
	./deploy/e2e_hardening.sh

e2e-integration: ## Run the integration-hub e2e: register adapters, emit events, fan-out dispatch counts (expects services up)
	./deploy/e2e_integration.sh

seed: ## Seed demo data (campaign + spin-wheel game + prizes + integration) via the reference admin BFF (expects services up)
	./deploy/seed.sh

docs: ## Run the Docusaurus docs site locally (architecture + flows, http://localhost:3000)
	cd docs/website && npm install && npm start

docs-build: ## Build the static docs site to docs/website/build
	cd docs/website && npm install && npm run build
