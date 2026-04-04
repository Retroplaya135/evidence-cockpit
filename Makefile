.PHONY: check build demo

check:
	go test ./...
	go vet ./...

build:
	go test -run '^$$' ./...

demo:
	go run ./cmd/evidence-import --input fixtures/evidence.csv
