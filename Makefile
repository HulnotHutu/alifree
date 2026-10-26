BINARY := bin/quota
PKG    := ./cmd/quota

.PHONY: build run test vet tidy fmt clean

build:
	go build -trimpath -ldflags="-s -w" -o $(BINARY) $(PKG)

run: build
	$(BINARY) serve

test:
	go test ./...

vet:
	go vet ./...

fmt:
	gofmt -l -w .

tidy:
	go mod tidy

clean:
	rm -rf bin
