.PHONY: test clean

all: test

test: clean
	go run gotest.tools/gotestsum@v1.12.0 -- -coverprofile=coverage.txt ./...

clean:
	git clean -Xdf
