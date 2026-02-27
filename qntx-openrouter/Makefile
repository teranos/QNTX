.PHONY: build clean test

BINARY=qntx-openrouter-plugin

build:
	go build -o $(BINARY) ./cmd/qntx-openrouter-plugin

test:
	go test -v ./...

clean:
	rm -f $(BINARY)
