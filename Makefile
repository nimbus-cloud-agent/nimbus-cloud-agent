.PHONY: web build bundle-harness clean

# Build the React frontend into web/dist, which the Go binary embeds.
web:
	cd web && npm ci && npm run build

# Build the single binary (run `make web` first for a UI-included build).
build:
	go build -o bin/nimbus-cloud-agent ./cmd/nimbus

# Opt-in only: download the official DeepSeek Harness package, keep its
# original LICENSE intact under third_party/deepseek-harness/, and record
# the version in third_party/deepseek-harness/NOTICE.md. This step is never
# run by default, so no third-party code is redistributed by accident.
bundle-harness:
	./scripts/bundle-harness.sh

clean:
	rm -rf bin web/dist
