BINARY := abmctl
VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -ldflags "-X main.version=$(VERSION)"

.PHONY: build run test install clean

build:
	go build $(LDFLAGS) -o $(BINARY) .

run: build
	./$(BINARY)

test:
	go test ./...

install:
	go install $(LDFLAGS) .

clean:
	rm -f $(BINARY)
