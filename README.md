# Template

One repository, one Go CLI. Go owns project actions and tests; Python calls a
small Rust worker; a Vite dashboard displays the same recorded test results.
The Go code uses only the standard library.

## Get started

Install Git, Go 1.24 or newer (the module selects Go 1.26.2), Node 22.12+ and npm,
Rust/cargo/rustfmt, and Python 3 using their official installers. Keep them on
`PATH`. Git works the same on Windows PowerShell, macOS Terminal, and Linux:

```sh
git clone --depth 1 https://github.com/timcash/template.git my-project
cd my-project
go run . install
go run . test
go run . dev
```

Cloning gets the source; `install` downloads language dependencies and UI
packages. This does not install the prerequisite toolchains or change system
settings. Open `http://127.0.0.1:5174/` for the dashboard. It starts no tests
until **Start tests** is clicked. Stop the development server with Ctrl+C.

## CLI

Run from the repository root. After `build`, `bin/template` (Windows:
`bin/template.exe`) accepts the same actions.

```sh
go run . help                         # commands and flags
go run . install                      # project dependencies
go run . lint                         # Go, Python, Rust, TypeScript checks
go run . format                       # format Go and Rust
go run . build                        # CLI, Rust worker, and web UI
go run . auth status --json            # nonsecret auth state (see below)
go run . auth help                    # auth contract and exit codes
go run . test                         # run and record all tests
go run . test --list                   # list without running
go run . test --filter TestPythonClient
go run . dev --port 5174               # local dashboard
go run . deploy                       # LOCAL bundle in deploy/local only
go run . clean                        # generated artifacts; retains run logs
```

## Authentication

`auth status` is provider-neutral. Its JSON fields are `provider`, `target`,
`kind`, `configured`, `verified`, `status`, and `next_step`. The stock template
returns `not_configured`, empty provider/target/kind, and false configured and
verified values. It is **not a working provider login**.

Exit 0 means verified (or help); 1 means unconfigured/unverified/unavailable;
2 means invalid syntax. `go run` wraps nonzero program exits; use the built
binary when scripts need exact exit codes.

Projects replace `authenticationStatus` in `cli.go` with a bounded adapter to
the provider's supported CLI or SDK and approved credential store. Verify the
intended target; distinguish machine/service identity from human login. A
cached token or reachable host is not authentication proof. Return only the
public status fields, never secrets or raw provider errors. Login, provisioning,
permission grants, and refresh require an explicit provider implementation.
The stock command reads no credentials, trusts no environment claims, opens no
browser, contacts no network, saves no tokens, and changes no access.

## How it works

- **`cli.go`:** one action dispatcher. Commands launch tools as argument vectors,
  not shell strings. Auth is separate from the offline lifecycle commands.
- **`cli_test.go`:** the test inventory. Add a Go test function; Go discovers it.
  Tests for Python, Rust, or UI launch those tools from Go. No duplicate list.
- **`test_runs.go`:** `test` consumes `go test -json`. Each execution gets a
  unique ID and immutable `logs/<test-id>.json` and `.log` files. A rerun keeps
  old logs. Direct `go test` works but skips this recording.
- **`ui/`:** Vite reads `logs/state.json` produced by the CLI. Cards show the
  test title, running/pass/fail, elapsed time, and a per-run Log link. The local
  Start tests button invokes the same CLI; visiting does not start work.
- **`python/` and `rust/`:** a typed Python client calls the Rust JSON arithmetic
  worker as a subprocess. No FFI or extra Python environment is required.

## Install the dashboard as an app

The dashboard has a web app manifest, 192/512-pixel PNG icons, and an Apple
Touch icon. On Chromium, use the browser's **Install app** option when offered.
On iPhone/iPad, open the page in Safari and use **Share → Add to Home Screen**, then turn on **Open as Web App**.
It is online-only: no service worker or offline cache stores tests, logs, or
authentication. Installing an icon does not grant login or network access.

`TestWebUIInstallablePWA` is included in `go run . test`. It builds the real
Vite output, starts a temporary loopback preview server, and checks the served
entry page, manifest, same-origin start URL, icon bytes/dimensions and MIME types.
This verifies the install metadata over HTTP, not a user's Home Screen tap or
browser-specific installation UI. Localhost is a trustworthy local context;
remote browser installation needs HTTPS and a separately authenticated host.
The development server remains loopback-only. Do not expose its test-running API
merely to install the PWA. A static build lacks the development test API.

## Make it yours

Rename the module in `go.mod`, the Rust package/library and matching import in
`rust/Cargo.toml` and `rust/src/main.rs`, and the package in `ui/package.json`.
Keep the help/README and contract tests aligned as commands are added.

`deploy` does not publish online. The loopback development dashboard is not an
authenticated network service. Do not expose it as one. The UI uses grayscale,
with red for failures. On Windows the runner discovers normal per-user Python
and Cargo installations; it changes only its own process PATH.
