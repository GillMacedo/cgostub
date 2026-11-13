.PHONY: test lint demo realworld

test:
	test -z "$$(gofmt -l plugin tests examples)"
	go vet ./...
	cd examples/photosvc && go vet ./...
	cd examples/sensord && go vet ./...
	cd examples/healthy && go vet ./...
	go test -race ./...

lint:
	claude plugin validate ./plugin --strict
	claude plugin validate . --strict

# Exit 1 means it found problems, which photosvc and sensord have on purpose.
demo:
	go run ./plugin/scripts --dir examples/photosvc --target linux/amd64,darwin/arm64,windows/amd64 ./... || test $$? -eq 1
	go run ./plugin/scripts --dir examples/sensord --target linux/amd64 ./... || test $$? -eq 1
	go run ./plugin/scripts --dir examples/healthy --target linux/amd64,darwin/arm64,windows/amd64 --std .

# Needs the network: downloads real modules and projects.
realworld:
	sh scripts/realworld.sh
