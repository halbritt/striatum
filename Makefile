SHELL := /usr/bin/env bash

MAKEFILE_DIR := $(abspath $(dir $(lastword $(MAKEFILE_LIST))))
GO_DIR := $(MAKEFILE_DIR)/go
VERSION := $(shell tr -d '[:space:]' < "$(MAKEFILE_DIR)/VERSION")
PREFIX ?= $(HOME)/.local
DIST_DIR ?= $(MAKEFILE_DIR)/dist

.PHONY: install uninstall build lint typecheck test smoke check installed-cli-check lane-isolation-check lane-isolation-check-ci release-check check-docs \
	go-build go-test go-vet go-release release-archives check-release-archives package-smoke

install: go-build
	mkdir -p "$(PREFIX)/bin"
	install -m 0755 "$(GO_DIR)/bin/striatum" "$(PREFIX)/bin/striatum"
	install -m 0755 "$(GO_DIR)/bin/striatumd" "$(PREFIX)/bin/striatumd"
	install -m 0755 "$(GO_DIR)/bin/striatum-supervisor-helper" "$(PREFIX)/bin/striatum-supervisor-helper"
	"$(PREFIX)/bin/striatum" daemon install --no-start
	"$(PREFIX)/bin/striatum" skills install
	@echo "==> attempting daemon start + health check (best effort)"
	@echo "    (a Postgres DSN must be configured before the daemon can bind;"
	@echo "     see ~/.config/striatum/daemon.toml — set postgres_url)"
	-"$(PREFIX)/bin/striatum" daemon install
	-"$(PREFIX)/bin/striatum" daemon status
	@echo "==> if 'doctor' is not ok, set postgres_url in ~/.config/striatum/daemon.toml"
	@echo "    then run: striatum daemon install && striatum doctor"

uninstall:
	-"$(PREFIX)/bin/striatum" daemon uninstall
	rm -f "$(PREFIX)/bin/striatum" "$(PREFIX)/bin/striatumd" "$(PREFIX)/bin/striatum-supervisor-helper"
	@echo "Removed binaries and systemd user unit. Left ~/.config/striatum/daemon.toml and data intact."

build: go-build

lint: go-vet

typecheck: go-test

test: go-test

smoke:
	"$(MAKEFILE_DIR)/scripts/go_fresh_clone_smoke.sh"

check: lint test

# Broken local doc links (frozen provenance under docs/rfcs, docs/records/_frozen, etc.
# is excluded via .check-docs-ignore). Standalone for now: a living-doc link
# backlog must be burned down before this can join `check`.
check-docs:
	python3 scripts/check_docs.py

installed-cli-check:
	STRIATUM_P3_INSTALLED_CLI=1 $(MAKE) -C "$(GO_DIR)" installed-cli-check

lane-isolation-check:
	"$(MAKEFILE_DIR)/scripts/check_lane_isolation_neg.sh"

# CI / operator wrapper for the RFC 0096 #87 lane-isolation gate (D244).
#
# The negative-control gate fundamentally requires host provisioning a stock
# runner cannot have (a dedicated PG-less lane OS user, passwordless `sudo -n
# -u`, and PostgreSQL `pg_hba.conf` reject rules — see
# docs/how-to/lane-sandbox.md). It is therefore *operator-provisioned
# hardening*, not mandatory-in-CI. This target is the legible entry point for a
# conditional CI job: it runs the real gate ONLY when the host advertises
# provisioning via STRIATUM_LANE_ISOLATION_HOST=1, and otherwise SKIPS LOUDLY so
# a green CI never falsely implies the isolation gate ran.
lane-isolation-check-ci:
	"$(MAKEFILE_DIR)/scripts/check_lane_isolation_ci.sh"

release-check: check release-archives check-release-archives package-smoke smoke

go-build:
	$(MAKE) -C "$(GO_DIR)" build

go-test:
	$(MAKE) -C "$(GO_DIR)" test

go-vet:
	$(MAKE) -C "$(GO_DIR)" lint

go-release:
	$(MAKE) -C "$(GO_DIR)" release

release-archives:
	"$(MAKEFILE_DIR)/scripts/build_go_release_archives.sh" --dist "$(DIST_DIR)"

check-release-archives:
	"$(MAKEFILE_DIR)/scripts/check_go_release_archives.sh" "$(DIST_DIR)"

package-smoke:
	"$(MAKEFILE_DIR)/scripts/go_package_smoke.sh"
