.DEFAULT_GOAL := build
.PHONY: generate check-generated web runtime build test test-runtime pack-runtime run setup-mac

generate:
	npm run protocol:generate

check-generated: generate
	npm run protocol:check

web:
	npm ci --prefix web
	npm run build --prefix web

runtime: generate
	npm run build --ignore-scripts --workspace nimbus-agent-runtime

build: runtime web
	go build -trimpath -o bin/nimbus-cloud-agent ./cmd/nimbus

test-runtime: runtime
	npm test --ignore-scripts --workspace nimbus-agent-runtime
	node --test scripts/tool-events.test.ts

test: test-runtime web
	go test -race ./...

pack-runtime: runtime
	mkdir -p release
	npm pack --ignore-scripts --workspace nimbus-agent-runtime --pack-destination release

run: build
	npm start

setup-mac:
	bash scripts/setup-macos.sh
