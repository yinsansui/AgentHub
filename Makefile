.PHONY: fmt check build-control-plane build-agent-pod build-linux-bins image-agent-pod image-control-plane images

fmt:
	gofmt -w cmd internal pkg

check: fmt
	go test ./...

build-control-plane:
	go build ./cmd/control-plane

build-agent-pod:
	go build ./cmd/agent-pod

build-linux-bins:
	mkdir -p .agenthub/bin
	CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags='-s -w' -o .agenthub/bin/agent-pod ./cmd/agent-pod
	CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags='-s -w' -o .agenthub/bin/control-plane ./cmd/control-plane

image-agent-pod: build-linux-bins
	docker build -f deploy/Dockerfile.agent-pod -t agenthub-pi-agent-pod:dev .

image-control-plane: build-linux-bins
	docker build -f deploy/Dockerfile.control-plane -t agenthub-control-plane:dev .

images: image-agent-pod image-control-plane
