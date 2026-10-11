# Build metadata embedded into the binary (see internal/version).
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT  ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo none)
DATE    ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS := -s -w \
	-X github.com/Mechas-Heart/Godoo/internal/version.Version=$(VERSION) \
	-X github.com/Mechas-Heart/Godoo/internal/version.Commit=$(COMMIT) \
	-X github.com/Mechas-Heart/Godoo/internal/version.Date=$(DATE)

# Nix only runs on Linux and macOS, so those are the release targets.
PLATFORMS := linux/amd64 linux/arm64 darwin/amd64 darwin/arm64

.PHONY: build install test vet fmt dist clean

build:
	CGO_ENABLED=0 go build -ldflags "$(LDFLAGS)" -o bin/godoo ./cmd/godoo

install:
	CGO_ENABLED=0 go install -ldflags "$(LDFLAGS)" ./cmd/godoo

test:
	go test ./...

vet:
	go vet ./...

fmt:
	gofmt -w .

# dist/godoo_<version>_<os>_<arch>.tar.gz + checksums.txt
dist: clean
	@mkdir -p dist
	@for platform in $(PLATFORMS); do \
		os=$${platform%/*}; arch=$${platform#*/}; \
		name=godoo_$(VERSION)_$${os}_$${arch}; \
		echo "building $$name"; \
		mkdir -p build/$$name; \
		CGO_ENABLED=0 GOOS=$$os GOARCH=$$arch go build -ldflags "$(LDFLAGS)" -o build/$$name/godoo ./cmd/godoo || exit 1; \
		cp README.md LICENSE NOTICE build/$$name/; \
		tar -C build -czf dist/$$name.tar.gz $$name; \
	done
	@cd dist && sha256sum *.tar.gz > checksums.txt
	@rm -rf build

clean:
	rm -rf bin build dist
