.PHONY: build build-arm build-arm64 clean

GO ?= go

build:
	$(GO) build -o bin/pigrow-client ./cmd/pigrow-client

build-arm:
	GOOS=linux GOARCH=arm GOARM=7 $(GO) build -o bin/pigrow-client-arm ./cmd/pigrow-client

build-arm64:
	GOOS=linux GOARCH=arm64 $(GO) build -o bin/pigrow-client-arm64 ./cmd/pigrow-client

clean:
	rm -rf bin/
