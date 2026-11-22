VERSION := v0.8.0
LDFLAGS := -s -w -X github.com/nodemode-dev/formnet/internal/app.Version=$(VERSION)
GO_BUILD := CGO_ENABLED=0 go build -trimpath -ldflags "$(LDFLAGS)"
TARGETS := linux/amd64 linux/arm64 darwin/amd64 darwin/arm64 windows/amd64

.PHONY: build web server release check test clean

build: web server

web:
	test -d web/node_modules || ./web/oc_install
	rm -rf web/dist/assets web/dist/index.html web/dist/favicon.png
	cd web && npm run build

server:
	$(GO_BUILD) -o bin/formnet ./cmd/formnet

release: web
	rm -rf release && mkdir -p release
	@for t in $(TARGETS); do \
		os=$${t%/*}; arch=$${t#*/}; ext=""; \
		if [ "$$os" = windows ]; then ext=".exe"; fi; \
		echo "building $$os/$$arch"; \
		GOOS=$$os GOARCH=$$arch $(GO_BUILD) -o release/formnet_$(VERSION)_$${os}_$${arch}$$ext ./cmd/formnet || exit 1; \
	done
	cd release && (sha256sum formnet_* > SHA256SUMS 2>/dev/null || shasum -a 256 formnet_* > SHA256SUMS)

check:
	gofmt -l . | (! grep .)
	go vet ./...
	go run ./cmd/formnet agents check
	test -d web/node_modules || ./web/oc_install
	cd web && npm run typecheck

test:
	go test -count=1 ./...

clean:
	rm -rf bin release web/dist/assets web/dist/index.html web/dist/favicon.png
