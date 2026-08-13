import {
  recordTerminalLiveOutput,
  shouldPauseTerminalLiveOutput,
  TERMINAL_LIVE_OUTPUT_IDLE_MS,
  TERMINAL_LIVE_OUTPUT_LIMIT_MS,
} from "./terminalHelpers";

function check(name: string, actual: unknown, expected: unknown) {
  if (JSON.stringify(actual) !== JSON.stringify(expected)) {
    throw new Error(`${name}: ${JSON.stringify(actual)} !== ${JSON.stringify(expected)}`);
  }
}

const started = recordTerminalLiveOutput(null, 100);
check("starts output window", started, { startedAt: 100, lastOutputAt: 100 });

const continued = recordTerminalLiveOutput(started, 100 + TERMINAL_LIVE_OUTPUT_IDLE_MS);
check("continues active output window", continued, {
  startedAt: 100,
  lastOutputAt: 100 + TERMINAL_LIVE_OUTPUT_IDLE_MS,
});

const restarted = recordTerminalLiveOutput(continued, 101 + 2 * TERMINAL_LIVE_OUTPUT_IDLE_MS);
check("restarts after output becomes idle", restarted, {
  startedAt: 101 + 2 * TERMINAL_LIVE_OUTPUT_IDLE_MS,
  lastOutputAt: 101 + 2 * TERMINAL_LIVE_OUTPUT_IDLE_MS,
});

check(
  "does not pause before limit",
  shouldPauseTerminalLiveOutput(continued, 100 + TERMINAL_LIVE_OUTPUT_LIMIT_MS - 1),
  false,
);

const stillStreaming = {
  startedAt: 100,
  lastOutputAt: 100 + TERMINAL_LIVE_OUTPUT_LIMIT_MS,
};
check(
  "pauses continuous output at limit",
  shouldPauseTerminalLiveOutput(stillStreaming, 100 + TERMINAL_LIVE_OUTPUT_LIMIT_MS),
  true,
);

check(
  "does not pause output that went idle",
  shouldPauseTerminalLiveOutput(
    { startedAt: 100, lastOutputAt: 100 + TERMINAL_LIVE_OUTPUT_LIMIT_MS - 2_000 },
    100 + TERMINAL_LIVE_OUTPUT_LIMIT_MS,
  ),
  false,
);

console.log("terminalHelpers tests passed");
