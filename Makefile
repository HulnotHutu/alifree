GOBUILD := go build -trimpath -ldflags="-s -w"

.PHONY: build web run test vet tidy fmt clean

# 前端产物：缺失时自动安装依赖并构建（go:embed 需要，见 webui.go）。
# 改了 React 代码后重新入库：make web && make build，或开发期用 Vite HMR。
web-ui/dist/index.html:
	cd web-ui && npm install && npm run build

web:
	cd web-ui && npm run build

build: web-ui/dist/index.html
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
