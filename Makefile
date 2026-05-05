.PHONY: fmt check build-control-plane build-agent-pod build-ts-runtime-host build-linux-bins image-agent-pod image-control-plane images smoke-real-runtime smoke-cancel-lifecycle

fmt:
	gofmt -w cmd internal pkg

check: fmt
	go test ./...
	cd runtimes/ts-runtime-host && npm ci && npm run build

build-control-plane:
	go build ./cmd/control-plane

build-agent-pod:
	go build ./cmd/agent-pod

build-ts-runtime-host:
	cd runtimes/ts-runtime-host && npm ci && npm run build

build-linux-bins:
	mkdir -p .agenthub/bin
	CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags='-s -w' -o .agenthub/bin/agent-pod ./cmd/agent-pod
	CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags='-s -w' -o .agenthub/bin/control-plane ./cmd/control-plane

image-agent-pod: build-linux-bins
	docker build -f deploy/Dockerfile.agent-pod -t agenthub-agent-pod:dev .

image-control-plane: build-linux-bins
	docker build -f deploy/Dockerfile.control-plane -t agenthub-control-plane:dev .

images: image-agent-pod image-control-plane

smoke-real-runtime:
	./scripts/smoke/real-runtime-golden-path.sh

smoke-cancel-lifecycle:
	./scripts/smoke/cancel-lifecycle.sh
