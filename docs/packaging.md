# Packaging

Nimbus ships as **two artifacts**: a Go executable with the compiled frontend, and the **`nimbus-agent-runtime`** npm package. The runtime explicitly depends on `@deepseek-ai/dsh@0.2.1-rc.5` and installs Harness through npm. Go does not embed, extract, or resolve Harness or runtime source code.

[Deployment guide](deployment.md) · [Runtime protocol](RUNTIME_RPC.md)

## Build from source

With Node.js 24.11+, Go 1.25+, npm and Make installed, run from the repository root:

```sh
npm ci
make
make pack-runtime
```

Outputs:

| Artifact | Purpose |
| --- | --- |
| `bin/nimbus-cloud-agent` | Go control plane and embedded React frontend |
| `release/nimbus-agent-runtime-0.3.0.tgz` | TypeScript runtime compiled to JavaScript, generated RPC SDK/schema, package metadata and README |

Make generates the shared protocol before compilation. Generated source, `dist`, binaries, and tarballs remain ignored by Git. A runtime build clears its old output directory, so removed adapters cannot remain in the package.

## Install the artifacts

On the destination machine, install Node.js 24.11+ and provision PostgreSQL first. Go is not required to run a prebuilt executable.

```sh
npm install -g ./nimbus-agent-runtime-0.3.0.tgz
nimbus-agent-runtime --version

# Use the executable for your operating system and architecture.
chmod +x ./nimbus-darwin-arm64
export NIMBUS_POSTGRES='postgres://nimbus:your-password@127.0.0.1:5432/nimbus?sslmode=disable'
export NIMBUS_E2B_API_KEY='your-e2b-api-key'
export NIMBUS_DEEPSEEK_API_KEY='your-deepseek-api-key'
./nimbus-darwin-arm64 serve
```

If using the locally built executable, substitute `./bin/nimbus-cloud-agent`. The npm global executable directory must be on PATH; alternatively select the installed runtime with `--runtime-command /absolute/path/to/nimbus-agent-runtime`.

Open http://127.0.0.1:8080. Go supervises the runtime and applies database migrations. The executable reads environment variables or its configuration file; the repository's key-file fallback and background helper belong to the source launcher.

**Runtime 0.3.0 uses protocol v3.** Upgrade both artifacts together. The package is not yet published to the public npm registry; install the tarball rather than assuming `npm install -g nimbus-agent-runtime` is available.

## Releases

The release workflow builds executables for macOS arm64/amd64 and Linux arm64/amd64 with `CGO_ENABLED=0`, alongside the runtime tarball and `SHA256SUMS`. Artifact names are `nimbus-darwin-arm64`, `nimbus-darwin-amd64`, `nimbus-linux-arm64`, and `nimbus-linux-amd64`.

E2B's Go adapter is part of the control plane. The runtime package contains no E2B SDK or PTY implementation. E2B supplies the cloud execution environment on every platform; no hypervisor or VM relay is packaged.
