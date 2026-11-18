package main

import (
	"bufio"
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
	"unicode"
)

// testRunEnv marks the `go test` process that the test action starts, so a test
// can never launch the action that is already running it.
const testRunEnv = "TEMPLATE_TEST_RUN"

// goTestTimeout outruns a cold install and build on a slow machine.
const goTestTimeout = "30m"

// stateFile is what the dashboard reads. The test and dev actions generate it;
// nothing else writes it, and it is never committed.
var stateFile = filepath.Join("logs", "state.json")

type testState struct {
	GeneratedAt string      `json:"generated_at"`
	Tests       []testEntry `json:"tests"`
}

type testEntry struct {
	Name      string `json:"name"`
	Title     string `json:"title"`
	Status    string `json:"status"`
	Elapsed   string `json:"elapsed"`
	Timestamp string `json:"timestamp"`
	TestID    string `json:"test-id"`
}

type testRunRecord struct {
	TestID      string `json:"test-id"`
	Name        string `json:"name"`
	Title       string `json:"title"`
	Command     string `json:"command"`
	Status      string `json:"status"`
	Timestamp   string `json:"timestamp"`
	Elapsed     string `json:"elapsed"`
	Artifact    string `json:"artifact"`
	CompletedAt string `json:"completed_at,omitempty"`
}

// goTestEvent is one line of `go test -json`.
type goTestEvent struct {
	Time    time.Time
	Action  string
	Test    string
	Output  string
	Elapsed float64
}

type activeTest struct {
	name    string
	log     *os.File
	record  testRunRecord
	started time.Time
}

// listTestNames asks the Go toolchain which tests exist, in source order.
func listTestNames(root, pattern string) ([]string, error) {
	if pattern == "" {
		pattern = ".*"
	}
	command := exec.Command("go", "test", "-list", pattern, ".")
	command.Dir = root
	output, err := command.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("go test -list %s: %w\n%s", pattern, err, output)
	}
	var names []string
	for _, line := range strings.Split(string(output), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "Test") && !strings.ContainsAny(line, " \t") {
			names = append(names, line)
		}
	}
	return names, nil
}

func newTestState(names []string) testState {
	var state testState
	for _, name := range names {
		state.Tests = append(state.Tests, newTestEntry(name))
	}
	return state
}

func newTestEntry(name string) testEntry {
	return testEntry{Name: name, Title: testTitle(name), Status: "pending", Elapsed: "0", Timestamp: "none"}
}

func (state *testState) entry(name string) *testEntry {
	for index := range state.Tests {
		if state.Tests[index].Name == name {
			return &state.Tests[index]
		}
	}
	state.Tests = append(state.Tests, newTestEntry(name))
	return &state.Tests[len(state.Tests)-1]
}

func writeTestState(root string, state *testState) error {
	state.GeneratedAt = time.Now().UTC().Format(time.RFC3339Nano)
	return writeJSONFile(filepath.Join(root, stateFile), state)
}

// runGoTests runs the selected tests and turns the event stream into live
// output, per-execution logs, and dashboard state.
func runGoTests(root string, state *testState, pattern string, stdout, stderr io.Writer) error {
	args := []string{"test", "-json", "-count=1", "-timeout", goTestTimeout}
	if pattern != "" {
		args = append(args, "-run", pattern)
	}
	command := exec.Command("go", append(args, ".")...)
	command.Dir = root
	command.Env = append(os.Environ(), testRunEnv+"=1")
	var toolErrors bytes.Buffer
	command.Stderr = &toolErrors
	events, err := command.StdoutPipe()
	if err != nil {
		return err
	}
	if err := command.Start(); err != nil {
		return err
	}
	failures, consumeErr := consumeGoTestEvents(root, state, events, stdout)
	if consumeErr != nil {
		_ = command.Process.Kill() // A stopped reader must not leave a blocked child.
	}
	waitErr := command.Wait()
	for _, line := range strings.Split(toolErrors.String(), "\n") {
		if line = strings.TrimRight(line, "\r"); line != "" {
			if err := writeRecord(stderr, nil, humanRecord(time.Time{}, "INFO", "child stderr: "+line)); err != nil && consumeErr == nil {
				consumeErr = err
			}
		}
	}
	if consumeErr != nil {
		return consumeErr
	}
	if len(failures) > 0 {
		return fmt.Errorf("%d test(s) failed: %s", len(failures), strings.Join(failures, ", "))
	}
	return waitErr
}

// consumeGoTestEvents reads `go test -json` lines and records what happened.
// Subtest output is folded into the log of the test that owns it.
func consumeGoTestEvents(root string, state *testState, events io.Reader, stdout io.Writer) ([]string, error) {
	active := map[string]*activeTest{}
	defer func() {
		for _, run := range active {
			_ = run.log.Close()
		}
	}()
	var failures []string
	scanner := bufio.NewScanner(events)
	scanner.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
	for scanner.Scan() {
		var event goTestEvent
		if err := json.Unmarshal(scanner.Bytes(), &event); err != nil {
			if err := writeRecord(stdout, nil, humanRecord(time.Time{}, "WARN", "unrecognized Go test output: "+scanner.Text())); err != nil {
				return failures, err
			}
			continue
		}
		owner := topLevelTest(event.Test)
		switch event.Action {
		case "output", "build-output":
			// test2json combines the test binary's stdout and stderr. Only the
			// separately captured Go command pipe has known stderr provenance.
			label := "test output: "
			if event.Action == "build-output" {
				label = "build output: "
			}
			for _, line := range strings.Split(event.Output, "\n") {
				if line = strings.TrimRight(line, "\r"); line == "" {
					continue
				}
				if err := writeRecord(stdout, active[owner], humanRecord(event.Time, "INFO", label+line)); err != nil {
					return failures, err
				}
			}
		case "run":
			if event.Test == "" || event.Test != owner {
				continue
			}
			run, err := beginTestRun(root, state, owner, event.Time)
			if err != nil {
				return failures, err
			}
			active[owner] = run
			if err := writeRecord(stdout, run, humanRecord(event.Time, "INFO", fmt.Sprintf("=== TEST-ID %s %s", owner, run.record.TestID))); err != nil {
				return failures, err
			}
		case "pass", "fail", "skip":
			run := active[owner]
			if run == nil || event.Test == "" || event.Test != owner {
				continue
			}
			level := "INFO"
			if event.Action == "fail" {
				level = "ERROR"
			} else if event.Action == "skip" {
				level = "WARN"
			}
			if err := writeRecord(stdout, run, humanRecord(event.Time, level, fmt.Sprintf("test %s %s (%.3fs)", owner, event.Action, event.Elapsed))); err != nil {
				return failures, err
			}
			if err := finishTestRun(root, state, run, event.Action, event.Elapsed, event.Time); err != nil {
				return failures, err
			}
			delete(active, owner)
			if event.Action == "fail" {
				failures = append(failures, owner)
			}
		}
	}
	// A panic or a killed process can end the stream mid-test. Close the books
	// so no row is left running forever.
	for name, run := range active {
		if err := writeRecord(stdout, run, humanRecord(time.Time{}, "ERROR", fmt.Sprintf("test %s ended without a Go result", name))); err != nil {
			return failures, err
		}
		if err := finishTestRun(root, state, run, "fail", time.Since(run.started).Seconds(), time.Time{}); err != nil {
			return failures, err
		}
		delete(active, name)
		failures = append(failures, name)
	}
	return failures, scanner.Err()
}

func beginTestRun(root string, state *testState, name string, when time.Time) (*activeTest, error) {
	var random [16]byte
	if _, err := rand.Read(random[:]); err != nil {
		return nil, err
	}
	id := hex.EncodeToString(random[:])
	if err := os.MkdirAll(filepath.Join(root, "logs"), 0o755); err != nil {
		return nil, err
	}
	log, err := os.OpenFile(filepath.Join(root, "logs", id+".log"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, err
	}
	started := time.Now()
	if when.IsZero() {
		when = started
	}
	entry := state.entry(name)
	entry.Status, entry.TestID = "running", id
	entry.Timestamp, entry.Elapsed = when.UTC().Format(time.RFC3339Nano), "0"
	run := &activeTest{name: name, log: log, started: started, record: testRunRecord{
		TestID:    id,
		Name:      name,
		Title:     entry.Title,
		Command:   fmt.Sprintf("go run . test --filter %q", "^"+name+"$"),
		Status:    "running",
		Timestamp: entry.Timestamp,
		Elapsed:   "0",
		Artifact:  "logs/" + id + ".log",
	}}
	if err := writeRunRecord(root, run.record); err != nil {
		_ = log.Close()
		return nil, err
	}
	if err := writeTestState(root, state); err != nil {
		_ = log.Close()
		return nil, err
	}
	return run, nil
}

func finishTestRun(root string, state *testState, run *activeTest, action string, elapsed float64, when time.Time) error {
	status := "fail"
	if action == "pass" || action == "skip" {
		status = action
	}
	seconds := fmt.Sprintf("%.3f", elapsed)
	entry := state.entry(run.name)
	entry.Status, entry.Elapsed = status, seconds
	run.record.Status, run.record.Elapsed = status, seconds
	if when.IsZero() {
		when = time.Now()
	}
	run.record.CompletedAt = when.UTC().Format(time.RFC3339Nano)
	if err := run.log.Close(); err != nil {
		return err
	}
	if err := writeRunRecord(root, run.record); err != nil {
		return err
	}
	return writeTestState(root, state)
}

func writeRunRecord(root string, record testRunRecord) error {
	return writeJSONFile(filepath.Join(root, "logs", record.TestID+".json"), record)
}

func topLevelTest(name string) string {
	if index := strings.Index(name, "/"); index >= 0 {
		return name[:index]
	}
	return name
}

// testTitle turns TestPythonCallsRustWorker into "Python Calls Rust Worker".
func testTitle(name string) string {
	runes := []rune(strings.TrimPrefix(name, "Test"))
	if len(runes) == 0 {
		return name
	}
	var title strings.Builder
	for index, char := range runes {
		next := ' '
		if index+1 < len(runes) {
			next = runes[index+1]
		}
		if index > 0 && unicode.IsUpper(char) && (!unicode.IsUpper(runes[index-1]) || unicode.IsLower(next)) {
			title.WriteRune(' ')
		}
		title.WriteRune(char)
	}
	return title.String()
}

func writeJSONFile(path string, value any) error {
	raw, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(path+".tmp", append(raw, '\n'), 0o644); err != nil {
		return err
	}
	// Windows readers and antivirus scanners can briefly deny replacement of an
	// open file. Keep the update atomic and retry only that transient error.
	for attempt := 0; ; attempt++ {
		err := os.Rename(path+".tmp", path)
		if runtime.GOOS != "windows" || !errors.Is(err, os.ErrPermission) || attempt >= 19 {
			return err
		}
		time.Sleep(25 * time.Millisecond)
	}
}
