[private]
help:
	@just --list

build:
	cd tools/marketplace-build && go build -o ../../bin/marketplace-build .

test:
	cd tools/marketplace-build && go test -coverprofile=coverage.out ./...
	node .github/scripts/test-plugins.js

release *args:
	./bin/marketplace-build {{args}} --dry-run
