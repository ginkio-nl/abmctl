BINARY := abmctl

.PHONY: build run test install clean

build:
	go build -o $(BINARY) .

run: build
	./$(BINARY)

test:
	go test ./...

install:
	go install .

clean:
	rm -f $(BINARY)
