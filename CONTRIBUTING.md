# Contributing to Tubo

Thanks for your interest in contributing.

## Reporting Issues

- [Report a Bug](https://github.com/endlessite/tubo/issues/new)
- [Security Issues](SECURITY.md) — do not open a public issue

## Development Setup

### Go CLI (`tubo-cli/`)

```bash
cd tubo-cli
go build -o tubo
./tubo send <file>
```

### Java Relay Server (`server/`)

```bash
cd server
mvn clean package
java -jar target/tubo-server-1.0.0-fat.jar
```

Set `TUBO_SERVER=http://localhost:8080` or run `tubo config server localhost:8080` to point the CLI to your local server.

### Tests

```bash
cd tubo-cli && go test ./...
TUBO_JAR=server/target/tubo-server-1.0.0-fat.jar TUBO_BIN=tubo-cli/tubo sh ci/e2e.sh
```

The end-to-end script starts a local relay and runs transfers between the CLI and `run.sh` in every direction. CI runs it on every pull request.

## Guidelines

1. **Keep it simple.** Tubo's value is its simplicity. Think twice before adding features.
2. **No new dependencies** without a very good reason. The Go CLI has one dependency (`gorilla/websocket`). Let's keep it that way.
3. **POSIX compliance** for shell scripts. `run.sh` must work on `dash`, `ash`, and any POSIX-compliant shell.
4. **No secrets on command lines.** Keys and passwords go through the environment or private temp files, never as arguments, because arguments are visible in `ps`.
5. **Protocol changes touch both sides.** `deriveCipherParams` in `tubo-cli/crypto.go` and `aes_ctr` in `run.sh` must stay identical; the unit tests check this against `openssl`.
6. **Test on multiple platforms.** The CLI must work on Linux (glibc + musl), macOS, and Windows.

## Pull Requests

1. Fork the repo and create a branch from `main`
2. Make your changes
3. Run the unit and end-to-end tests
4. Open a PR with a clear description of what and why

By contributing, you agree that your contributions will be licensed under the MIT License.
