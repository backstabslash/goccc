.PHONY: build install test vet fmt lint cyclo check ci

build:
	go build -o goccc .

install:
	go install .

test:
	go test ./...

vet:
	go vet ./...

fmt:
	@test -z "$$(gofmt -s -l .)" || (echo "gofmt -s found issues:"; gofmt -s -l .; exit 1)

lint:
	golangci-lint run

cyclo:
	@test -z "$$(gocyclo -over 15 $$(find . -name '*.go' ! -name '*_test.go'))" || \
		(echo "gocyclo found functions with complexity > 15:"; \
		gocyclo -over 15 $$(find . -name '*.go' ! -name '*_test.go'); exit 1)

check: vet fmt test lint cyclo

ci: check install
