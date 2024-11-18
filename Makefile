.PHONY: test

all: clean test

test: clean
	@echo "Running tests..."
	@go run gotest.tools/gotestsum@v1.12.0 -- -coverprofile=coverage.txt ./...

clean:
	git clean -Xdf
