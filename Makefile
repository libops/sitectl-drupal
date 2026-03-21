.PHONY: build deps lint test work install

BINARY_NAME=sitectl-drupal
INSTALL_DIR ?= $(or $(dir $(shell which $(BINARY_NAME) 2>/dev/null)),/usr/local/bin/)

deps: work
	go mod tidy

build: deps
	go build -o $(BINARY_NAME) .

install: work build
	sudo cp $(BINARY_NAME) $(INSTALL_DIR)$(BINARY_NAME)

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
