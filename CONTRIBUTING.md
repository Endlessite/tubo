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

## Guidelines

1. **Keep it simple.** Tubo's value is its simplicity. Think twice before adding features.
2. **No new dependencies** without a very good reason. The Go CLI has one dependency (`gorilla/websocket`). Let's keep it that way.
3. **POSIX compliance** for shell scripts. `run.sh` must work on `dash`, `ash`, and any POSIX-compliant shell.
4. **Test on multiple platforms.** The CLI must work on Linux (glibc + musl), macOS, and Windows.

## Pull Requests

1. Fork the repo and create a branch from `main`
2. Make your changes
3. Test locally (both Go CLI and shell script modes)
4. Open a PR with a clear description of what and why

By contributing, you agree that your contributions will be licensed under the MIT License.
