.PHONY: build test vet clean linux-amd64 linux-arm64 docker

APP := simplewebshell-remoteupdate

build:
	go build -trimpath -ldflags='-s -w' -o $(APP) .

test:
	go test ./...

vet:
	go vet ./...

linux-amd64:
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags='-s -w' -o dist/$(APP)-linux-amd64 .

linux-arm64:
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -trimpath -ldflags='-s -w' -o dist/$(APP)-linux-arm64 .

docker:
	docker build --build-arg TARGETARCH=$${TARGETARCH:-amd64} -t simplewebshell-remoteupdate:latest .

clean:
	rm -rf $(APP) dist
