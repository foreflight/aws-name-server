all: build
build: build-linux
build-linux:
	GOARCH=amd64 GOOS=linux go build .