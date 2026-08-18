VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || printf '0.0.0-development')
COMMIT ?= $(shell git rev-parse --short HEAD 2>/dev/null || printf 'uncommitted')
BUILD_DATE ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
PACKAGE_VERSION ?= $(shell node scripts/package-version.js "$(VERSION)")
LDFLAGS := -s -w -X git.qtech.cn/ai/everyline-cli/internal/build.Version=$(VERSION) -X git.qtech.cn/ai/everyline-cli/internal/build.Commit=$(COMMIT) -X git.qtech.cn/ai/everyline-cli/internal/build.Date=$(BUILD_DATE)

.PHONY: build test vet release-assets release-check package-check clean

build:
	go build -trimpath -ldflags "$(LDFLAGS)" -o bin/everyline-cli ./cmd/everyline-cli

test:
	go test -race ./...
	npm test

vet:
	go vet ./...
	go mod verify
	go mod tidy -diff

release-assets:
	VERSION="$(PACKAGE_VERSION)" COMMIT="$(COMMIT)" BUILD_DATE="$(BUILD_DATE)" sh scripts/build-release-assets.sh

package-check:
	sh tests/release/verify-assets.sh
	EXPECTED_PACKAGE_VERSION="$(PACKAGE_VERSION)" sh tests/release/package-dry-run.sh
	EXPECTED_VERSION="$(PACKAGE_VERSION)" sh tests/release/local-install.sh

release-check: test vet release-assets package-check

clean:
	rm -rf bin dist coverage.out
