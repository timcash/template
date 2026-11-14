# Template

A small, self-contained project scaffold. Go owns the actions and the tests,
Python owns a typed client, Rust owns a tiny arithmetic worker, and Vite
provides a local test dashboard.

Copy this repository to start a new project. Everything the project needs lives
in this one folder, and the Go module imports only the standard library.

## Commands

Run these from the repository root:

```bash
# Show the actions and flags.
go run . help

# Download Go/Rust modules and install UI packages.
go run . install

# Check Go, Python, Rust, and TypeScript.
go run . lint

# Format Go and Rust sources.
go run . format

# Build the Go binary, release Rust worker, and Vite bundle.
go run . build

# Run every test in cli_test.go, in order.
go run . test

# Run one test, or any Go test name pattern.
go run . test --filter TestPythonClient

# Print the test names without running them.
go run . test --list

# Remove generated state.
go run . clean

# Build and copy a safe local bundle to deploy/local.
go run . deploy

# Start the dashboard; it lists pending tests and waits for Start tests.
go run . dev --port 5174
```

`build` also writes a standalone `bin/template` binary, which takes the same
actions without the `go run` prefix.

## Tests

`cli_test.go` is the test inventory. Go runs its tests in source order, so the
file reads top to bottom: the four workflow actions first, then the Python,
Rust, and UI suites, then the contract tests that keep this scaffold honest.
Adding a test is adding a Go function — there is no list to update.

Go is also the only thing that launches a test. A suite written in another
language is a Go test that starts that tool:

```go
func TestRustWorker(t *testing.T) {
	runTool(t, "cargo", "test", "--manifest-path", "rust/Cargo.toml")
}
```

`go run . test` runs `go test -json`, turns the event stream into live
terminal output, and records every execution. `go test` on its own works too;
it just skips the recording.

Every execution gets a unique `test-id` and writes `logs/<test-id>.json` and
`logs/<test-id>.log`, which are kept when the test is rerun. The dashboard
reads one generated file, `logs/state.json`, which `dev` and `test` write and
nothing else touches, so the CLI and the dashboard can never disagree about a
run. `clean` removes that file; `dev` and `test` write it again.

## Dashboard

```bash
go run . dev
```

Open `http://127.0.0.1:5174/`. The page lists every test as pending and runs
nothing until you press **Start tests**, which shells out to the same
`go run . test`. Each card updates from pending to running and then passed or
failed, with live elapsed time.

A card shows the test's name, its Go function, status, elapsed time, and a
**Log** link to `/logs/?test-id=<id>`. That page holds the command to rerun it,
the timestamps, and the full captured output, including any failure.

## Python and Rust

Python starts `rust/target/release/json_math` as a subprocess, passes an
operation and two integers as arguments, and decodes the worker's JSON stdout.
This is deliberately simpler than FFI or PyO3: there is no platform-specific
extension module, Python environment, ABI coupling, or code-generation step.
The subprocess boundary is easy to test and the JSON response remains explicit.

```bash
python3 -m python.client 20 22
# {"result": 42}
```

The Rust library, the Python client, and the real subprocess interaction each
have their own test.

## Layout

```text
.
  cli.go          actions and the CLI entry point
  cli_test.go     the test inventory
  test_runs.go    runs go test and records each execution
  python/         Python client and unit/interaction tests
  rust/           dependency-free Rust library and JSON binary
  ui/             Vite test dashboard and its tests
```

## Starting a new project from this template

The project boundary is the repository. Copy it, then rename in three places:

1. `go.mod` — the module path.
2. `rust/Cargo.toml` — the package and library names, and the matching `use`
   in `rust/src/main.rs`.
3. `ui/package.json` — the UI package name.

`go.mod` intentionally has no `require` or `replace` directives, and the Go
code imports only the standard library. Keep the contract tests at the bottom
of `cli_test.go` passing as you add your own tests above them.

## Requirements and limitations

- Go, Node/npm, Rust/cargo/rustfmt, and Python 3 must be on `PATH`. The project
  checks and invokes those tools directly.
- The runner also discovers Cargo in `CARGO_HOME/bin` or `~/.cargo/bin` and
  standard per-user Python installations on Windows. Python candidates are
  checked before use so Microsoft Store shortcuts are skipped. Discovery
  updates only the runner's process environment, not system settings.
- The interface uses grayscale, with red reserved for failures.
- The sample protocol intentionally supports one integer `add` operation. It
  demonstrates the process boundary without becoming an RPC framework.
- `deploy` is local-only. It does not contact hosts or services.
- The dashboard is a development server, not an authenticated network service;
  its default listen address is loopback.
