# package.json 是当前版本的唯一来源；CI 仍可通过 VERSION 注入 tag 或提交构建版本。
VERSION ?= $(shell node -p 'require("./package.json").version')
COMMIT ?= $(shell git rev-parse --short HEAD 2>/dev/null || printf 'uncommitted')
BUILD_DATE ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
UPDATE_MANIFEST_URL ?=
PACKAGE_VERSION ?= $(shell node scripts/package-version.js "$(VERSION)")
LDFLAGS := -s -w -X git.qtech.cn/ai/everyline-cli/internal/build.Version=$(VERSION) -X git.qtech.cn/ai/everyline-cli/internal/build.Commit=$(COMMIT) -X git.qtech.cn/ai/everyline-cli/internal/build.Date=$(BUILD_DATE) -X git.qtech.cn/ai/everyline-cli/internal/build.UpdateManifestURL=$(UPDATE_MANIFEST_URL)

.PHONY: build test vet skill-assets release-assets release-check package-check package clean

build:
	go build -trimpath -ldflags "$(LDFLAGS)" -o bin/everyline-cli ./cmd/everyline-cli

test:
	go test -race ./...
	npm test

vet:
	go vet ./...
	go mod verify
	go mod tidy -diff

skill-assets:
	sh scripts/build-skill-bundles.sh
	sh tests/release/verify-skill-bundles.sh

release-assets: skill-assets
	VERSION="$(PACKAGE_VERSION)" COMMIT="$(COMMIT)" BUILD_DATE="$(BUILD_DATE)" sh scripts/build-release-assets.sh

# 本地交付递增正式版本的 patch 号，再让子 make 读取新版本构建全部制品。
# CI 发布已有标签时仍使用 release-check/npm pack，不再次递增标签版本。
package:
	npm version patch --no-git-tag-version
	$(MAKE) release-assets VERSION="$$(node -p 'require("./package.json").version')" PACKAGE_VERSION="$$(node -p 'require("./package.json").version')"
	node scripts/pack-release.js dist

package-check: skill-assets
	sh tests/release/verify-assets.sh
	EXPECTED_PACKAGE_VERSION="$(PACKAGE_VERSION)" sh tests/release/package-dry-run.sh
	EXPECTED_VERSION="$(PACKAGE_VERSION)" sh tests/release/local-install.sh

release-check: test vet release-assets package-check

clean:
	rm -rf bin dist coverage.out
