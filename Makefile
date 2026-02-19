install:
	go install .

test:
	go vet ./...
	go test ./...

lint:
	golangci-lint run

ci: test install lint
