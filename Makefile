GO ?= go

.PHONY: run test lint e2e demo docker

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
