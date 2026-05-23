BIN := bin/xsh

.PHONY: build clean fmt vet

build:
	go build -o $(BIN) ./cmd/xsh

clean:
	rm -rf bin/

fmt:
	go fmt ./...

vet:
	go vet ./...
