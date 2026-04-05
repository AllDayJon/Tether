.PHONY: build install test vet clean

## build: compile the tether binary into the project directory
build:
	go build -o tether .

## install: install tether to $GOPATH/bin
install:
	go install .

## test: run all tests
test:
	go test ./...

## vet: run go vet static analysis
vet:
	go vet ./...

## clean: remove the local binary
clean:
	rm -f tether

## help: print this help
help:
	@grep -E '^## ' Makefile | sed 's/## //'
