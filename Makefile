.PHONY: build test integration
build:
	go build -o bin/rdda ./cmd/rdda
test:
	go test ./...
integration:
	go test -tags=integration ./test/integration/...
