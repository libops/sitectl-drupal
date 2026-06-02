.PHONY: build deps lint test work install

BINARY_NAME=sitectl-drupal

deps: work
	go mod tidy

build:
	go build -o $(BINARY_NAME) .

install: build
	mv $(BINARY_NAME) /usr/local/bin

lint:
	go fmt ./...
	golangci-lint run

	@if command -v json5 > /dev/null 2>&1; then \
		echo "Running json5 validation on renovate.json5"; \
		json5 --validate renovate.json5 > /dev/null; \
	else \
		echo "json5 not found, skipping renovate validation"; \
	fi

test: build
	go test -v -race ./...

work:
	./scripts/use-go-work.sh
