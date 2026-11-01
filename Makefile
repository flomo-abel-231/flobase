# FloBase developer commands.

.PHONY: env up down logs release-check build dashboard api dev-db dev-api dev-dashboard test clean

DOCKER_SOCKET ?= $(shell docker context inspect --format '{{.Endpoints.docker.Host}}' | sed 's|unix://||')

env: ## Write .env with random secrets (keeps an existing .env)
	@test -f .env && echo ".env exists, leaving it" || \
	  sed -e "s/^FLOBASE_API_TOKEN=.*/FLOBASE_API_TOKEN=$$(openssl rand -hex 24)/" \
	      -e "s/^FLOBASE_PLATFORM_DB_PASSWORD=.*/FLOBASE_PLATFORM_DB_PASSWORD=$$(openssl rand -hex 24)/" \
	      .env.example > .env && echo "Wrote .env. Your API token: $$(grep ^FLOBASE_API_TOKEN .env | cut -d= -f2)"

COMPOSE_DEV = docker compose -f docker-compose.yml -f docker-compose.build.yml

up: env ## Build from source and start FloBase on http://localhost:28470
	FLOBASE_VERSION=dev $(COMPOSE_DEV) up -d --build

down: ## Stop FloBase (project databases keep running)
	docker compose down

logs:
	docker compose logs -f flobase

release-check: ## Lint the shell scripts and workflows like CI does
	docker run --rm -v "$$PWD:/src" -w /src koalaman/shellcheck:v0.10.0 scripts/install.sh scripts/upgrade.sh
	docker run --rm -v "$$PWD:/repo" -w /repo rhysd/actionlint:1.7.7

build: dashboard api ## Build the dashboard and the control-plane binary

dashboard:
	npm ci && npm run build

api:
	cd control-plane && go build -o bin/flobase-api ./cmd/flobase-api

test:
	cd control-plane && go vet ./... && go test ./...
	npm run build

# Local development: platform database in Docker, API and dashboard on the host.
dev-db:
	docker run -d --name flobase-dev-db -p 5433:5432 \
	  -e POSTGRES_USER=flobase -e POSTGRES_PASSWORD=flobase -e POSTGRES_DB=flobase postgres:17-alpine

dev-api: ## Run the API on :28470 against the dev database
	cd control-plane && FLOBASE_API_TOKEN=$${FLOBASE_API_TOKEN:-dev-token-change-me} \
	  FLOBASE_MODE=$${FLOBASE_MODE:-cloud} FLOBASE_DOCKER_SOCKET=$(DOCKER_SOCKET) go run ./cmd/flobase-api

dev-dashboard: ## Run the dashboard on :5173, proxying /api to :28470
	npm run dev

clean:
	rm -rf dashboard/dist control-plane/bin
