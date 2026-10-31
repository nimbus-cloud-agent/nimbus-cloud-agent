# Packaging

Build the UI before the Go binary (`npm run build`). The executable embeds `web/dist` and Nimbus's trusted supervisor scripts. The official DeepSeek Harness is separately installed at exactly `0.2.1-rc.5` through the declared npm dependency and is not included in the executable.

Distribute the binary together with `package.json`, `package-lock.json`, and instructions to run `npm ci` in the application directory. Node 24.11+ and PostgreSQL are required. On first startup without a `runtime/` directory, Nimbus extracts its embedded scripts; their Node resolution finds the application `node_modules` directory. Source checkouts use the checked-in `runtime/` scripts, so rebuild for release after changing them.

Supported build targets: macOS arm64/amd64 and Linux arm64/amd64 (`CGO_ENABLED=0`). E2B supplies execution sandboxes on all platforms. No Linux VM relay or hypervisor is packaged.
