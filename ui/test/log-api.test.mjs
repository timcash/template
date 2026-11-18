import assert from "node:assert/strict";
import { appendFile, mkdtemp, mkdir, readFile, rm, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { dirname, resolve } from "node:path";
import test from "node:test";
import { fileURLToPath } from "node:url";

import { createServer, loadConfigFromFile } from "vite";

const uiDir = resolve(dirname(fileURLToPath(import.meta.url)), "..");

test("the dashboard serves generated state and per-execution logs", async (context) => {
  const fixture = await mkdtemp(resolve(tmpdir(), "template-log-api-"));
  const logsDir = resolve(fixture, "logs");
  const statePath = resolve(logsDir, "state.json");
  await mkdir(logsDir);
  const firstId = "a".repeat(32);
  const secondId = "b".repeat(32);
  const firstLog = "2026-01-01T00:00:01Z INFO test_runs.go:120 first execution output\n";
  const secondStart = "2026-01-01T00:00:01Z INFO test_runs.go:120 second execution output\n";
  const secondFailure = "2026-01-01T00:00:02Z ERROR test_runs.go:135 second execution failed\n";
  const secondLog = secondStart + secondFailure;
  for (const [id, status, log] of [
    [firstId, "pass", firstLog],
    [secondId, "running", secondStart],
  ]) {
    await writeFile(
      resolve(logsDir, `${id}.json`),
      JSON.stringify({
        "test-id": id,
        name: "TestPythonClient",
        title: "Python Client",
        command: 'go run . test --filter "^TestPythonClient$"',
        artifact: `logs/${id}.log`,
        status,
        elapsed: "0",
        timestamp: "none",
      }),
    );
    await writeFile(resolve(logsDir, `${id}.log`), log);
  }
  await writeFile(
    statePath,
    `${JSON.stringify({
      generated_at: "2026-01-01T00:00:00Z",
      tests: [{ name: "TestPythonClient", title: "Python Client", status: "running", elapsed: "0", timestamp: "none", "test-id": secondId }],
    })}\n`,
    "utf8",
  );

  const previousPath = process.env.TEMPLATE_STATE_PATH;
  process.env.TEMPLATE_STATE_PATH = statePath;
  const loaded = await loadConfigFromFile({ command: "serve", mode: "test" }, resolve(uiDir, "vite.config.ts"));
  if (previousPath === undefined) {
    delete process.env.TEMPLATE_STATE_PATH;
  } else {
    process.env.TEMPLATE_STATE_PATH = previousPath;
  }
  assert.ok(loaded, "vite config should load");

  const server = await createServer({
    ...loaded.config,
    configFile: false,
    root: uiDir,
    logLevel: "silent",
    server: { ...loaded.config.server, host: "127.0.0.1", port: 0, strictPort: false },
  });
  context.after(async () => {
    await server.close();
    await rm(fixture, { recursive: true, force: true });
  });
  await server.listen();
  const address = server.httpServer?.address();
  assert.ok(address && typeof address === "object");

  const base = `http://127.0.0.1:${address.port}`;
  const tests = (await (await fetch(`${base}/api/tests`)).json()).tests;
  assert.equal(tests.length, 1);
  assert.equal(tests[0].name, "TestPythonClient");
  assert.equal(tests[0]["test-id"], secondId);

  const readRun = async (id) => {
    const response = await fetch(`${base}/api/test-runs/${id}`);
    assert.equal(response.status, 200);
    return (await response.json()).run;
  };
  const details = await readRun(firstId);
  assert.equal(details.log, firstLog);
  assert.equal(details.name, "TestPythonClient");
  assert.match(details.command, /--filter/);
  assert.equal((await readRun(secondId)).log, secondStart);
  await appendFile(resolve(logsDir, `${secondId}.log`), secondFailure);
  assert.equal((await readRun(secondId)).log, secondLog);
  const secondRecord = resolve(logsDir, `${secondId}.json`);
  const activeRun = JSON.parse(await readFile(secondRecord, "utf8"));
  await writeFile(secondRecord, JSON.stringify({ ...activeRun, status: "fail" }));
  const failed = await readRun(tests[0]["test-id"]);
  assert.equal(failed.status, "fail");
  assert.equal(failed.artifact, `logs/${secondId}.log`);
  assert.equal(failed.log, secondLog);
  assert.equal((await readRun(firstId))["test-id"], firstId);
  const raw = await fetch(`${base}/api/test-runs/${firstId}/log`);
  assert.equal(raw.headers.get("content-type"), "text/plain; charset=utf-8");
  assert.equal(await raw.text(), firstLog);
  assert.equal(await (await fetch(`${base}/api/test-runs/${secondId}/log`)).text(), secondLog);
  assert.equal((await fetch(`${base}/api/test-runs/${"c".repeat(32)}`)).status, 404);
  assert.equal((await fetch(`${base}/api/test-runs/not-a-run`)).status, 400);
  assert.equal((await fetch(`${base}/api/test-runs/..%2Fstate.json`)).status, 400);
  const page = await fetch(`${base}/logs/?test-id=${firstId}`);
  assert.equal(page.status, 200);
  assert.match(page.headers.get("content-type"), /text\/html/);
});
