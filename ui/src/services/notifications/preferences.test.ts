import assert from "node:assert/strict";
import {
  getQuietHours,
  isChannelEnabled,
  isQuietHours,
  setChannelEnabled,
  setQuietHours,
} from "./preferences";

const values = new Map<string, string>();
Object.defineProperty(globalThis, "localStorage", {
  configurable: true,
  value: {
    getItem: (key: string) => values.get(key) ?? null,
    setItem: (key: string, value: string) => values.set(key, value),
  },
});

function at(hours: number, minutes: number): Date {
  const date = new Date(2026, 6, 19, hours, minutes);
  return date;
}

assert.deepEqual(getQuietHours(), { enabled: false, start: "22:00", end: "07:00" });
assert.equal(isQuietHours(at(23, 0)), false);

setQuietHours({ enabled: true, start: "22:00", end: "07:00" });
assert.equal(isQuietHours(at(22, 0)), true);
assert.equal(isQuietHours(at(6, 59)), true);
assert.equal(isQuietHours(at(12, 0)), false);

setQuietHours({ enabled: true, start: "09:00", end: "17:00" });
assert.equal(isQuietHours(at(10, 0)), true);
assert.equal(isQuietHours(at(18, 0)), false);

values.clear();
Object.defineProperty(globalThis, "Notification", {
  configurable: true,
  value: { permission: "granted" },
});
assert.equal(isChannelEnabled("browser"), true);
setChannelEnabled("browser", false);
assert.equal(isChannelEnabled("browser"), false);

console.log("notification preference tests passed");
