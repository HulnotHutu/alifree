GOBUILD := go build -trimpath -ldflags="-s -w"

.PHONY: build run test vet tidy fmt clean

build:
	$(GOBUILD) -o bin/quota ./cmd/quota
	$(GOBUILD) -o bin/aihelpme ./cmd/aihelpme
	$(GOBUILD) -o bin/aihelpmed ./cmd/aihelpmed

run: build
	bin/quota serve

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
