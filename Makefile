GO ?= go

.PHONY: run test lint e2e demo docker release deploy-pi

run:
	$(GO) run ./cmd/pointvote

test:
	$(GO) test -race ./...

lint:
	$(GO) vet ./...
	@unformatted="$$(gofmt -l .)"; if [ -n "$$unformatted" ]; then \
		echo "gofmt needed:"; echo "$$unformatted"; exit 1; fi

e2e:
	$(GO) build -o bin/pointvote ./cmd/pointvote
	./e2e/api_test.sh bin/pointvote

demo:
	./demo/estimate.sh

docker:
	docker build -t pointvote .

# Cross-compile for the Pi (aarch64 userland).
release:
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 $(GO) build -trimpath \
		-ldflags="-s -w" -o dist/pointvote-linux-arm64 ./cmd/pointvote

# Ship the binary and restart. Wipes live rooms; documented, accepted.
deploy-pi: release
	@test -n "$(PI_HOST)" || { echo "set PI_HOST, e.g. PI_HOST=pi@point-vote"; exit 1; }
	scp dist/pointvote-linux-arm64 $(PI_HOST):/tmp/pointvote
	ssh $(PI_HOST) 'sudo install -m 0755 /tmp/pointvote /usr/local/bin/pointvote && sudo systemctl restart pointvote && rm /tmp/pointvote'
