.PHONY: web build test run setup-mac
web:
	npm ci --prefix web
	npm run build --prefix web
build: web
	go build -trimpath -o bin/nimbus-cloud-agent ./cmd/nimbus
test:
	go test ./...
	npm test
run:
	npm start
setup-mac:
	bash scripts/setup-macos.sh
