COMPOSE ?= docker compose
GIT ?= git
GIT_REMOTE ?= origin
LOG_TAIL ?= 200
RELEASE_BRANCH ?= main
TAG_MESSAGE ?= Release $(TAG)

.PHONY: up down restart log build run release

up:
	$(MAKE) down
	$(MAKE) build
	$(MAKE) run

restart:
	bash deploy/local-restart.sh

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
	@case "$(TAG)" in v* ) ;; * ) echo "TAG must start with v. Example: TAG=v2026.06.25.1" >&2; exit 1;; esac
	@case "$(TAG)" in *[!A-Za-z0-9._-]* ) echo "TAG may only contain letters, numbers, dot, underscore, and hyphen." >&2; exit 1;; esac
	@command -v $(GIT) >/dev/null 2>&1 || (echo "git is required." >&2; exit 1)
	@test "$$($(GIT) branch --show-current)" = "$(RELEASE_BRANCH)" || (echo "Release must run from $(RELEASE_BRANCH)." >&2; exit 1)
	@test -z "$$($(GIT) status --porcelain)" || (echo "Worktree must be clean before release." >&2; $(GIT) status --short; exit 1)
	@$(GIT) fetch $(GIT_REMOTE) $(RELEASE_BRANCH) --tags
	@test "$$($(GIT) rev-parse HEAD)" = "$$($(GIT) rev-parse $(GIT_REMOTE)/$(RELEASE_BRANCH))" || (echo "Local HEAD must match $(GIT_REMOTE)/$(RELEASE_BRANCH) before release." >&2; exit 1)
	@if $(GIT) rev-parse -q --verify "refs/tags/$(TAG)" >/dev/null; then echo "Local tag already exists: $(TAG)" >&2; exit 1; fi
	@if $(GIT) ls-remote --exit-code --tags $(GIT_REMOTE) "refs/tags/$(TAG)" >/dev/null 2>&1; then echo "Remote tag already exists: $(TAG)" >&2; exit 1; fi
	@$(GIT) tag -a "$(TAG)" -m "$(TAG_MESSAGE)"
	@$(GIT) push $(GIT_REMOTE) "$(TAG)"
	@echo "Pushed tag $(TAG). GitHub Actions deploy will run from the tag push."
