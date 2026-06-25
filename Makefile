COMPOSE ?= docker compose
GH ?= gh
GITHUB_REPO ?= khiemnd777/manleai
LOG_TAIL ?= 200
RELEASE_REF ?= main
WORKFLOW ?= ci-cd.yml

.PHONY: up down restart log build run release

up:
	$(MAKE) down
	$(MAKE) build
	$(MAKE) run

restart:
	$(MAKE) down
	$(MAKE) build
	$(MAKE) run

down:
	$(COMPOSE) down --remove-orphans

build:
	$(COMPOSE) build

run:
	$(COMPOSE) up -d

log:
	$(COMPOSE) logs -f --tail=$(LOG_TAIL)

release:
	@test -n "$(TAG)" || (echo "TAG is required. Usage: make release TAG=v2026.06.25.1" >&2; exit 1)
	@case "$(TAG)" in *[!A-Za-z0-9._-]* ) echo "TAG may only contain letters, numbers, dot, underscore, and hyphen." >&2; exit 1;; esac
	@command -v $(GH) >/dev/null 2>&1 || (echo "gh CLI is required. Install GitHub CLI and run gh auth login." >&2; exit 1)
	@$(GH) workflow run $(WORKFLOW) --repo $(GITHUB_REPO) --ref $(RELEASE_REF) -f tag="$(TAG)"
	@echo "Triggered $(WORKFLOW) on $(RELEASE_REF) for $(TAG)"
