import { spawnSync } from "node:child_process";

// Run each isolated runtime group in a fresh process, in reverse declaration
// order. This proves groups do not depend on module-level scenario state while
// keeping execution deterministic and failure output tied to one group.
const groups = [
  "reconnect replay synchronization epochs and stale events",
  "pending limits interruptions and send failures",
  "request correlation and subscription handle caches",
  "connection and authentication lifecycle",
  "codec and wire decoding",
  "identity token wire decoding",
  "connection state observers are isolated from lifecycle transitions",
  "protocol contracts and standalone subscription handles",
];

for (const group of groups) {
  const pattern = `^${group.replace(/[.*+?^${}()|[\]\\]/g, "\\$&")}$`;
  const result = spawnSync(
    process.execPath,
    [
      "--test",
      "--test-concurrency=1",
      `--test-name-pattern=${pattern}`,
      "test/runtime-behavior.test.mjs",
    ],
    { stdio: "inherit" },
  );
  if (result.error) {
    throw result.error;
  }
  if (result.status !== 0) {
    process.exit(result.status ?? 1);
  }
}
