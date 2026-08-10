import assert from "node:assert/strict";
import type { NotificationEvent } from "../../../types";
import { browserNotificationHandler, sendTestBrowserNotification } from "./browser";

class FakeNotification {
  static permission = "granted";
  static instances: FakeNotification[] = [];

  onclick: (() => void) | null = null;
  closed = false;

  constructor(
    readonly title: string,
    readonly options: NotificationOptions,
  ) {
    FakeNotification.instances.push(this);
  }

  close() {
    this.closed = true;
  }
}

let focused = true;
const assigned: string[] = [];
Object.defineProperty(globalThis, "Notification", { configurable: true, value: FakeNotification });
Object.defineProperty(globalThis, "document", {
  configurable: true,
  value: {
    hidden: false,
    hasFocus: () => focused,
  },
});
Object.defineProperty(globalThis, "window", {
  configurable: true,
  value: {
    __SHELLEY_INIT__: { hostname: "local-shell" },
    location: {
      href: "http://localhost:8002/c/current",
      origin: "http://localhost:8002",
      assign: (url: string) => assigned.push(url),
    },
    focus: () => {
      focused = true;
    },
  },
});

const event: NotificationEvent = {
  type: "agent_done",
  conversation_id: "conv-1",
  timestamp: new Date().toISOString(),
  payload: {
    hostname: "remote.exe.xyz",
    model: "gpt-5.6-luna",
    conversation_title: "herds",
    conversation_url: "https://remote.exe.xyz/c/herds",
    final_response: "Finished the requested implementation.",
  },
};

browserNotificationHandler(event);
assert.equal(FakeNotification.instances.length, 0, "focused tab stays quiet");

focused = false;
browserNotificationHandler(event);
assert.equal(
  FakeNotification.instances.length,
  1,
  "unfocused tab receives completion notification",
);
assert.match(FakeNotification.instances[0].options.body || "", /^gpt-5\.6-luna:/);
FakeNotification.instances[0].onclick?.();
assert.equal(assigned[0], "http://localhost:8002/c/herds#turn-completion-summary");
assert.equal(FakeNotification.instances[0].closed, true);

focused = true;
assert.equal(sendTestBrowserNotification(), true, "explicit test bypasses focus suppression");
assert.equal(FakeNotification.instances.length, 2);

console.log("browser notification tests passed");
