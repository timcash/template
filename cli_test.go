package main

// cli_test.go is the test inventory. Go runs these tests in the order they
// appear here, so the file reads top to bottom: the four workflow actions
// first, then the Python, Rust, and UI suites that Go launches, then the
// contract tests that keep this scaffold honest. Add a test where it belongs
// in that sequence and the dashboard picks it up with no other change.

import (
	"bytes"
	"encoding/json"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestInstallDependencies(t *testing.T) {
	runAction(t, "install")
}

func TestLintAllSources(t *testing.T) {
	runAction(t, "lint")
}

func TestBuildArtifacts(t *testing.T) {
	runAction(t, "build")
}

func TestDeployLocalBundle(t *testing.T) {
	runAction(t, "deploy")
}

func TestUIUnitTests(t *testing.T) {
	runTool(t, "npm", "--prefix", "ui", "test")
}

func TestWebUIInstallablePWA(t *testing.T) {
	runTool(t, "npm", "--prefix", "ui", "run", "build")
	runTool(t, "node", "--test", "ui/acceptance/pwa-install.mjs")
}

func TestPythonClient(t *testing.T) {
	runPython(t, "-m", "unittest", "python.test_client.PythonClientTests")
}

func TestRustWorker(t *testing.T) {
	runTool(t, "cargo", "test", "--manifest-path", "rust/Cargo.toml")
}

func TestPythonCallsRustWorker(t *testing.T) {
	runPython(t, "-m", "unittest", "python.test_interop.RustInteropTests")
}

func TestHelpListsEveryAction(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := Run([]string{"help"}, &stdout, &stderr); code != 0 {
		t.Fatalf("Run(help) code = %d, stderr = %s", code, stderr.String())
	}
	for _, action := range actionNames(t) {
		if !strings.Contains(stdout.String(), action) {
			t.Errorf("help is missing %q:\n%s", action, stdout.String())
		}
	}
	stdout.Reset()
	stderr.Reset()
	if code := Run([]string{"not-an-action"}, &stdout, &stderr); code != 2 {
		t.Fatalf("unknown action code = %d, want 2", code)
	}
}

func TestReadmeDocumentsEveryAction(t *testing.T) {
	readme, err := os.ReadFile("README.md")
	if err != nil {
		t.Fatal(err)
	}
	documented := map[string]bool{}
	for _, line := range strings.Split(string(readme), "\n") {
		fields := strings.Fields(strings.TrimSpace(line))
		if len(fields) >= 4 && fields[0] == "go" && fields[1] == "run" && fields[2] == "." {
			documented[fields[3]] = true
		}
	}
	known := map[string]bool{}
	for _, action := range actionNames(t) {
		known[action] = true
		if !documented[action] {
			t.Errorf("README does not show `go run . %s`", action)
		}
	}
	for action := range documented {
		if !known[action] {
			t.Errorf("README shows `go run . %s`, which is not an action", action)
		}
	}
}

func TestTestActionWillNotRunInsideItself(t *testing.T) {
	t.Setenv(testRunEnv, "1")
	var stdout, stderr bytes.Buffer
	if code := Run([]string{"test"}, &stdout, &stderr); code == 0 {
		t.Fatal("test started a second time inside its own run")
	}
	if !strings.Contains(stderr.String(), "already running") {
		t.Fatalf("unexpected failure: %s", stderr.String())
	}
}

func TestGoTestOutputDrivesRunState(t *testing.T) {
	root := t.TempDir()
	state := newTestState([]string{"TestAlpha", "TestBeta"})
	var stdout bytes.Buffer
	failures, err := consumeGoTestEvents(root, &state, strings.NewReader(strings.Join([]string{
		`{"Action":"run","Test":"TestAlpha"}`,
		`{"Action":"output","Test":"TestAlpha","Output":"alpha output\n"}`,
		`{"Action":"output","Test":"TestAlpha/with_a_subtest","Output":"subtest output\n"}`,
		`{"Action":"pass","Test":"TestAlpha/with_a_subtest","Elapsed":0.5}`,
		`{"Action":"pass","Test":"TestAlpha","Elapsed":1.5}`,
		`{"Action":"run","Test":"TestBeta"}`,
		`{"Action":"output","Test":"TestBeta","Output":"beta exploded\n"}`,
		`{"Action":"fail","Test":"TestBeta","Elapsed":0.25}`,
		`{"Action":"output","Output":"FAIL\n"}`,
	}, "\n")), &stdout)
	if err != nil {
		t.Fatal(err)
	}
	if len(failures) != 1 || failures[0] != "TestBeta" {
		t.Fatalf("failures = %v, want [TestBeta]", failures)
	}
	for _, marker := range []string{"alpha output", "subtest output", "beta exploded", "FAIL"} {
		if !strings.Contains(stdout.String(), marker) {
			t.Errorf("stdout is missing %q:\n%s", marker, stdout.String())
		}
	}

	alpha, beta := state.Tests[0], state.Tests[1]
	if alpha.Title != "Alpha" || alpha.Status != "pass" || alpha.Elapsed != "1.500" {
		t.Fatalf("alpha entry = %+v", alpha)
	}
	if beta.Status != "fail" || beta.Elapsed != "0.250" {
		t.Fatalf("beta entry = %+v", beta)
	}
	if len(alpha.TestID) != 32 || alpha.TestID == beta.TestID {
		t.Fatalf("executions must get distinct IDs: %q %q", alpha.TestID, beta.TestID)
	}

	log := readFileString(t, filepath.Join(root, "logs", alpha.TestID+".log"))
	if !strings.Contains(log, "alpha output") || !strings.Contains(log, "subtest output") {
		t.Fatalf("subtest output was not folded into its parent log: %q", log)
	}
	if strings.Contains(log, "beta exploded") || strings.Contains(log, "FAIL") {
		t.Fatalf("log picked up output from another test: %q", log)
	}
	record := readRunRecord(t, root, alpha.TestID)
	if record.Name != "TestAlpha" || record.Status != "pass" || record.CompletedAt == "" {
		t.Fatalf("run record = %+v", record)
	}
	if !strings.Contains(record.Command, "TestAlpha") || record.Artifact != "logs/"+alpha.TestID+".log" {
		t.Fatalf("run record cannot be reproduced or found: %+v", record)
	}

	written := readTestStateFile(t, root)
	if len(written.Tests) != 2 || written.Tests[1].Status != "fail" || written.GeneratedAt == "" {
		t.Fatalf("dashboard state was not written: %+v", written)
	}
}

func TestEachRunKeepsItsOwnLog(t *testing.T) {
	root := t.TempDir()
	stream := strings.Join([]string{
		`{"Action":"run","Test":"TestOnly"}`,
		`{"Action":"output","Test":"TestOnly","Output":"EXECUTION\n"}`,
		`{"Action":"pass","Test":"TestOnly","Elapsed":0.1}`,
	}, "\n")

	var ids []string
	for execution := 1; execution <= 2; execution++ {
		state := newTestState([]string{"TestOnly"})
		if _, err := consumeGoTestEvents(root, &state, strings.NewReader(stream), &bytes.Buffer{}); err != nil {
			t.Fatal(err)
		}
		ids = append(ids, state.Tests[0].TestID)
	}
	if ids[0] == ids[1] {
		t.Fatal("the rerun reused the first execution ID")
	}
	for _, id := range ids {
		if record := readRunRecord(t, root, id); record.TestID != id || record.Status != "pass" {
			t.Fatalf("an earlier run record was overwritten: %+v", record)
		}
		if log := readFileString(t, filepath.Join(root, "logs", id+".log")); log != "EXECUTION\n" {
			t.Fatalf("an earlier run log was overwritten: %q", log)
		}
	}
}

func TestCleanRemovesGeneratedState(t *testing.T) {
	root := newFixtureRoot(t)
	archived := filepath.Join(root, "logs", "0123456789abcdef0123456789abcdef.log")
	for _, path := range []string{
		filepath.Join(root, "bin", "template"),
		filepath.Join(root, "deploy", "local", "manifest.json"),
		filepath.Join(root, "rust", "target", "release", "json_math"),
		filepath.Join(root, "ui", "dist", "index.html"),
		filepath.Join(root, "ui", "node_modules", ".marker"),
		filepath.Join(root, "python", "__pycache__", "client.pyc"),
		filepath.Join(root, stateFile),
		archived,
	} {
		writeFixture(t, path, "generated\n")
	}
	withWorkingDirectory(t, root)

	var stdout, stderr bytes.Buffer
	if code := Run([]string{"clean"}, &stdout, &stderr); code != 0 {
		t.Fatalf("Run(clean) code = %d\nstdout:\n%s\nstderr:\n%s", code, stdout.String(), stderr.String())
	}
	for _, path := range []string{
		"bin", "deploy",
		filepath.Join("rust", "target"),
		filepath.Join("ui", "dist"),
		filepath.Join("ui", "node_modules"),
		filepath.Join("python", "__pycache__"),
		stateFile,
	} {
		if _, err := os.Stat(filepath.Join(root, path)); !os.IsNotExist(err) {
			t.Errorf("generated path still exists: %s (err=%v)", path, err)
		}
	}
	if readFileString(t, archived) != "generated\n" {
		t.Fatal("clean removed an archived run log")
	}
	if !fileExists(filepath.Join(root, "cli_test.go")) {
		t.Fatal("clean removed the test inventory")
	}
}

func TestDashboardReadsGeneratedState(t *testing.T) {
	mainSource := readFileString(t, filepath.Join("ui", "src", "main.ts"))
	viteSource := readFileString(t, filepath.Join("ui", "vite.config.ts"))
	for _, marker := range []string{
		`data-testid="start-tests"`,
		`startButton?.addEventListener("click"`,
		`setInterval(() =>`,
		`statusLabel(test.status)`,
		`elapsedSeconds(test)`,
	} {
		if !strings.Contains(mainSource, marker) {
			t.Errorf("dashboard source is missing %q", marker)
		}
	}
	for _, marker := range []string{
		`pathname === "/api/tests"`,
		`pathname === "/api/tests/run"`,
		`"logs", "state.json"`,
	} {
		if !strings.Contains(viteSource, marker) {
			t.Errorf("dashboard server is missing %q", marker)
		}
	}
	// The dashboard reads generated state and shells out to the CLI. It must
	// never write state itself, or the two could disagree about a run.
	for _, forbidden := range []string{"writeFile(", "rename("} {
		if strings.Contains(viteSource, forbidden) {
			t.Errorf("dashboard server writes test state with %q; only the CLI may", forbidden)
		}
	}
	if strings.Contains(mainSource, `void startAllTests();`) {
		t.Fatal("the dashboard must not start tests while mounting")
	}
}

func TestTemplateIsSelfContained(t *testing.T) {
	goModText := readFileString(t, "go.mod")
	modulePath := "github.com/advanced-metal-research/" + "template"
	if !strings.Contains(goModText, "module "+modulePath) {
		t.Fatalf("unexpected module declaration:\n%s", goModText)
	}
	for _, directive := range []string{"require ", "replace ", "exclude "} {
		if strings.Contains(goModText, "\n"+directive) {
			t.Fatalf("go.mod must not contain a %s directive:\n%s", strings.TrimSpace(directive), goModText)
		}
	}

	skipDirectories := map[string]bool{
		"bin": true, "deploy": true, "dist": true, "logs": true,
		"node_modules": true, "target": true, "__pycache__": true,
	}
	sourceExtensions := map[string]bool{
		".go": true, ".js": true, ".mjs": true, ".py": true, ".rs": true, ".toml": true, ".ts": true,
	}
	err := filepath.WalkDir(".", func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() && path != "." && skipDirectories[entry.Name()] {
			return filepath.SkipDir
		}
		if entry.IsDir() || !sourceExtensions[filepath.Ext(path)] {
			return nil
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if strings.Contains(string(raw), "/"+"internal/") {
			t.Errorf("%s reaches outside the template", path)
		}
		if filepath.Ext(path) != ".go" {
			return nil
		}
		file, err := parser.ParseFile(token.NewFileSet(), path, raw, parser.ImportsOnly)
		if err != nil {
			return err
		}
		for _, spec := range file.Imports {
			importPath, err := strconv.Unquote(spec.Path.Value)
			if err != nil {
				return err
			}
			if strings.Contains(strings.Split(importPath, "/")[0], ".") {
				t.Errorf("%s imports non-standard package %q", path, importPath)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	viteConfig := readFileString(t, filepath.Join("ui", "vite.config.ts"))
	if strings.Contains(viteConfig, "repoRoot") {
		t.Fatal("the dashboard must invoke this module, not reach up to a parent repository")
	}
	for _, marker := range []string{`spawn("go", ["run", ".", "test"]`, "cwd: commandRoot"} {
		if !strings.Contains(viteConfig, marker) {
			t.Errorf("the dashboard is missing self-contained marker %q", marker)
		}
	}
}

// Helpers.

func runAction(t *testing.T, action string, flags ...string) {
	t.Helper()
	if code := Run(append([]string{action}, flags...), os.Stdout, os.Stderr); code != 0 {
		t.Fatalf("go run . %s exited %d", action, code)
	}
}

func runTool(t *testing.T, name string, args ...string) {
	t.Helper()
	root, err := projectRoot()
	if err != nil {
		t.Fatal(err)
	}
	if err := runExternal(root, os.Stdout, os.Stderr, name, args...); err != nil {
		t.Fatal(err)
	}
}

// runPython uses the same verified interpreter the lint action uses, rather
// than a python3 alias that may open the Microsoft Store on Windows.
func runPython(t *testing.T, args ...string) {
	t.Helper()
	python, err := pythonExecutable()
	if err != nil {
		t.Fatal(err)
	}
	runTool(t, python, args...)
}

// actionNames reads the Actions block of the help text, so help stays the one
// place every action is listed.
func actionNames(t *testing.T) []string {
	t.Helper()
	var names []string
	listing := false
	for _, line := range strings.Split(usage, "\n") {
		if strings.HasPrefix(line, "Actions:") {
			listing = true
			continue
		}
		if !listing {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) == 0 {
			break
		}
		names = append(names, fields[0])
	}
	if len(names) == 0 {
		t.Fatal("the help text has no Actions block")
	}
	return names
}

func newFixtureRoot(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	writeFixture(t, filepath.Join(root, "go.mod"), "module example.test/template\n")
	writeFixture(t, filepath.Join(root, "cli.go"), "package main\n")
	writeFixture(t, filepath.Join(root, "cli_test.go"), "package main\n")
	writeFixture(t, filepath.Join(root, "ui", "package.json"), "{}\n")
	writeFixture(t, filepath.Join(root, "python", "__init__.py"), "")
	return root
}

func writeFixture(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatal(err)
	}
}

func withWorkingDirectory(t *testing.T, directory string) {
	t.Helper()
	previous, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(directory); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(previous); err != nil {
			t.Fatal(err)
		}
	})
}

func readFileString(t *testing.T, path string) string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func readRunRecord(t *testing.T, root, testID string) testRunRecord {
	t.Helper()
	var record testRunRecord
	if err := json.Unmarshal([]byte(readFileString(t, filepath.Join(root, "logs", testID+".json"))), &record); err != nil {
		t.Fatal(err)
	}
	return record
}

func readTestStateFile(t *testing.T, root string) testState {
	t.Helper()
	var state testState
	if err := json.Unmarshal([]byte(readFileString(t, filepath.Join(root, stateFile))), &state); err != nil {
		t.Fatal(err)
	}
	return state
}

func TestAuthStatusIsExplicitlyUnconfigured(t *testing.T) {
	for _, args := range [][]string{{"auth", "status"}, {"auth", "status", "--json"}} {
		var out, err bytes.Buffer
		if code := Run(args, &out, &err); code != 1 {
			t.Fatalf("%v: code %d, want 1", args, code)
		}
		if !strings.Contains(out.String(), "not_configured") {
			t.Fatalf("missing status: %s", &out)
		}
		if len(args) == 3 {
			var s struct {
				Provider, Target, Kind, Status, NextStep string
				Configured, Verified                     bool
			}
			var fields map[string]any
			if e := json.Unmarshal(out.Bytes(), &fields); e != nil {
				t.Fatal(e)
			}
			if e := json.Unmarshal(out.Bytes(), &s); e != nil {
				t.Fatal(e)
			}
			if s.Configured || s.Verified || s.Provider != "" || s.Target != "" || s.Kind != "" {
				t.Fatalf("false assurance: %+v", s)
			}
			next, ok := fields["next_step"].(string)
			if !ok || next == "" || len(fields) != 7 {
				t.Fatalf("invalid schema: %v", fields)
			}
		}
	}
}

func TestAuthHelpAndUsage(t *testing.T) {
	for _, args := range [][]string{{"auth"}, {"auth", "help"}, {"auth", "--help"}, {"auth", "status", "--help"}} {
		var out, err bytes.Buffer
		if code := Run(args, &out, &err); code != 0 || !strings.Contains(out.String(), "auth status") {
			t.Fatalf("%v: code %d: %s %s", args, code, &out, &err)
		}
	}
	for _, args := range [][]string{{"auth", "login"}, {"auth", "status", "--token", "sentinel-secret"}, {"auth", "status", "extra"}, {"auth", "help", "extra"}, {"auth", "status", "--json=garbage"}} {
		var out, err bytes.Buffer
		if code := Run(args, &out, &err); code != 2 {
			t.Fatalf("%v: code %d, want 2", args, code)
		}
		if strings.Contains(out.String()+err.String(), "sentinel-secret") {
			t.Fatal("echoed argument")
		}
	}
}

func TestAuthStatusIgnoresAmbientCredentialClaims(t *testing.T) {
	t.Setenv("TEMPLATE_AUTH_TOKEN", "sentinel-secret")
	t.Setenv("TEMPLATE_AUTH_VERIFIED", "true")
	var out, err bytes.Buffer
	if code := Run([]string{"auth", "status", "--json"}, &out, &err); code != 1 {
		t.Fatal(code)
	}
	if strings.Contains(out.String()+err.String(), "sentinel-secret") || !strings.Contains(out.String(), `"verified":false`) {
		t.Fatal("ambient claim trusted or leaked")
	}
}
