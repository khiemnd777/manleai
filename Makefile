COMPOSE ?= docker compose
LOG_TAIL ?= 200

.PHONY: up down restart log build run

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
