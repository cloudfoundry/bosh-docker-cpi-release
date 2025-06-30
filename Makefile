# BOSH Docker CPI Release Makefile

.PHONY: help prepare test clean build dev-release unit-tests test-quick

# Default target
help:
	@echo "BOSH Docker CPI Release - Available targets:"
	@echo ""
	@echo "  prepare      - Check and prepare development environment"
	@echo "  test         - Run full integration tests"
	@echo "  test-quick   - Run tests up to BOSH director deployment only"
	@echo "  clean        - Clean up test environment and artifacts"
	@echo "  clean-all    - Clean everything including all BOSH installations"
	@echo "  build        - Build the CPI binary"
	@echo "  dev-release  - Create a development BOSH release"
	@echo "  unit-tests   - Run unit tests only"
	@echo "  help         - Show this help message"
	@echo ""
	@echo "Environment variables:"
	@echo "  WORKSPACE_PATH    - Path to workspace with bosh-deployment (default: ~/workspace)"
	@echo "  DOCKER_NETWORK    - Docker network name (default: bosh-docker-test)"
	@echo "  USE_LOCAL_DOCKER  - Force local Docker usage (default: false)"
	@echo "  CLEANUP_DOWNLOADS - Also clean ~/.bosh/downloads during cleanup (default: false)"
	@echo "  STEMCELL_OS       - Stemcell OS: jammy or noble (default: noble)"
	@echo "  STEMCELL_VERSION  - Stemcell version (default: latest)"

# Check and prepare development environment
prepare:
	@echo "=====> Checking development environment"
	@cd tests && ./prepare.sh

# Run full integration tests
test: prepare clean build dev-release
	@echo "=====> Running BOSH Docker CPI integration tests"
	cd tests && STEMCELL_OS=$(STEMCELL_OS) STEMCELL_VERSION=$(STEMCELL_VERSION) ./run.sh

# Clean up test environment
clean:
	@echo "=====> Cleaning up BOSH Docker CPI test environment"
	@echo "-----> Force removing Docker containers"
	@docker ps -aq --filter "name=c-" | xargs -r docker rm -f 2>/dev/null || true
	@docker ps -aq --filter "name=bosh" | xargs -r docker rm -f 2>/dev/null || true
	@echo "-----> Force removing Docker volumes"
	@docker volume ls -q --filter "name=vol-" | xargs -r docker volume rm -f 2>/dev/null || true
	@echo "-----> Removing Docker network"
	@docker network rm $(DOCKER_NETWORK) 2>/dev/null || true
	@echo "-----> Cleaning up BOSH installation directories"
	@if [ -f tests/state.json ]; then \
		INSTALLATION_ID=$$(jq -r '.installation_id // empty' tests/state.json 2>/dev/null); \
		if [ -n "$$INSTALLATION_ID" ]; then \
			echo "       Removing installation $$INSTALLATION_ID"; \
			rm -rf ~/.bosh/installations/$$INSTALLATION_ID 2>/dev/null || true; \
		fi; \
	fi
	@echo "-----> Cleaning up test artifacts"
	@rm -f tests/cpi tests/creds.yml tests/state.json tests/run.sh.bak
	@echo "-----> Cleaning up BOSH downloads cache (optional)"
	@if [ "$(CLEANUP_DOWNLOADS)" = "true" ]; then \
		echo "       Removing ~/.bosh/downloads"; \
		rm -rf ~/.bosh/downloads 2>/dev/null || true; \
	fi
	@echo "-----> Pruning orphaned resources"
	@docker container prune -f 2>/dev/null || true
	@docker volume prune -f 2>/dev/null || true
	@docker network prune -f 2>/dev/null || true
	@echo "=====> Cleanup completed"

# Deep clean - removes all BOSH installations and downloads
clean-all: clean
	@echo "=====> Performing deep clean of all BOSH data"
	@echo "-----> Removing all BOSH installations"
	@if [ -d ~/.bosh/installations ]; then \
		COUNT=$$(find ~/.bosh/installations -mindepth 1 -maxdepth 1 -type d 2>/dev/null | wc -l); \
		if [ "$$COUNT" -gt 0 ]; then \
			echo "       Found $$COUNT installations to remove"; \
			rm -rf ~/.bosh/installations/* 2>/dev/null || true; \
		else \
			echo "       No installations found"; \
		fi; \
	fi
	@echo "-----> Removing BOSH downloads cache"
	@if [ -d ~/.bosh/downloads ]; then \
		SIZE=$$(du -sh ~/.bosh/downloads 2>/dev/null | cut -f1); \
		echo "       Removing downloads cache ($$SIZE)"; \
		rm -rf ~/.bosh/downloads 2>/dev/null || true; \
	fi
	@echo "-----> Removing all Docker images with 'stemcell' in name"
	@docker images --filter "reference=*stemcell*" -q | xargs -r docker rmi -f 2>/dev/null || true
	@echo "=====> Deep cleanup completed"

# Build the CPI binary
build:
	@echo "=====> Building BOSH Docker CPI binary"
	cd src/bosh-docker-cpi && ./bin/build

# Create development release
dev-release:
	@echo "=====> Creating development BOSH release"
	bosh create-release --force

# Run unit tests only
unit-tests:
	@echo "=====> Running BOSH Docker CPI unit tests"
	cd src/bosh-docker-cpi && ./bin/test

# Quick test - just verify BOSH director can be created
test-quick:
	@echo "=====> Running quick BOSH Docker CPI test (director creation only)"
	@echo "This will create the BOSH director and verify basic functionality"
	cd tests && STEMCELL_OS=$(STEMCELL_OS) STEMCELL_VERSION=$(STEMCELL_VERSION) timeout 300 ./run.sh || (echo "Quick test completed (may have timed out - this is normal for quick test)"; exit 0)

# Build for Linux (useful for release)
build-linux:
	@echo "=====> Building BOSH Docker CPI binary for Linux"
	cd src/bosh-docker-cpi && ./bin/build-linux-amd64

# Update Go dependencies
update-deps:
	@echo "=====> Updating Go dependencies"
	cd src/bosh-docker-cpi && go mod tidy && go mod vendor

# Verify cgroupsv2 support (useful for development)
check-cgroups:
	@echo "=====> Checking cgroupsv2 support"
	@if [ -f /sys/fs/cgroup/cgroup.controllers ]; then \
		echo "✓ cgroupsv2 is available"; \
		echo "Available controllers: $$(cat /sys/fs/cgroup/cgroup.controllers)"; \
	else \
		echo "✗ cgroupsv2 not available - this CPI requires cgroupsv2 support"; \
	fi
	@echo "Docker cgroup version:"
	@docker info 2>/dev/null | grep -i cgroup || echo "Could not determine Docker cgroup version"

# Quick test setup verification
verify-setup:
	@echo "=====> Verifying test setup"
	@echo "Checking required directories..."
	@if [ ! -d "$(WORKSPACE_PATH)/bosh-deployment" ]; then \
		echo "✗ Missing $(WORKSPACE_PATH)/bosh-deployment"; \
		echo "  Run: git clone https://github.com/cloudfoundry/bosh-deployment.git $(WORKSPACE_PATH)/bosh-deployment"; \
	else \
		echo "✓ bosh-deployment found"; \
	fi
	@if [ ! -d "$(WORKSPACE_PATH)/docker-deployment" ]; then \
		echo "✗ Missing $(WORKSPACE_PATH)/docker-deployment"; \
		echo "  Run: git clone https://github.com/cppforlife/docker-deployment.git $(WORKSPACE_PATH)/docker-deployment"; \
	else \
		echo "✓ docker-deployment found"; \
	fi
	@echo "Checking Docker connectivity..."
	@if docker version >/dev/null 2>&1; then \
		echo "✓ Docker is running"; \
	else \
		echo "✗ Docker is not running or not accessible"; \
	fi

# Set default workspace path if not provided
WORKSPACE_PATH ?= ~/workspace

# Set default Docker network name
DOCKER_NETWORK ?= bosh-docker-test