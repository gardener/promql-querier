// SPDX-FileCopyrightText: Contributors to the Gardener project
// SPDX-License-Identifier: Apache-2.0

import { describe, expect, it } from "vitest";
import {
  autoStepSeconds,
  formatNanos,
  parseDurationSeconds,
  parseEndTime,
  parseWallClockUTC,
} from "./step";

describe("parseDurationSeconds", () => {
  it("parses bare numbers as seconds", () => {
    expect(parseDurationSeconds("300")).toBe(300);
    expect(parseDurationSeconds("1.5")).toBe(1.5);
  });

  it("parses single units", () => {
    expect(parseDurationSeconds("1s")).toBe(1);
    expect(parseDurationSeconds("30m")).toBe(1800);
    expect(parseDurationSeconds("1h")).toBe(3600);
    expect(parseDurationSeconds("1d")).toBe(86400);
    expect(parseDurationSeconds("1w")).toBe(604800);
    expect(parseDurationSeconds("1y")).toBe(31536000);
    expect(parseDurationSeconds("500ms")).toBe(0.5);
  });

  it("sums compound durations", () => {
    expect(parseDurationSeconds("1h30m")).toBe(5400);
    expect(parseDurationSeconds("1d12h")).toBe(129600);
  });

  it("trims surrounding whitespace", () => {
    expect(parseDurationSeconds("  1h  ")).toBe(3600);
  });

  it("returns NaN for empty or unparseable input", () => {
    expect(parseDurationSeconds("")).toBeNaN();
    expect(parseDurationSeconds("   ")).toBeNaN();
    expect(parseDurationSeconds("abc")).toBeNaN();
  });
});

describe("parseEndTime", () => {
  it("resolves blank input to roughly now", () => {
    const before = Date.now() / 1000;
    const result = parseEndTime("");
    const after = Date.now() / 1000;
    expect(result).toBeGreaterThanOrEqual(before);
    expect(result).toBeLessThanOrEqual(after);
  });

  it("parses unix-second numbers verbatim", () => {
    expect(parseEndTime("1700000000")).toBe(1700000000);
  });

  it("parses RFC3339 timestamps to unix seconds", () => {
    expect(parseEndTime("2023-11-14T22:13:20Z")).toBe(1700000000);
  });

  it("interprets a bare wall-clock string as UTC", () => {
    expect(parseEndTime("2023-11-14 22:13:20")).toBe(1700000000);
  });

  it("returns NaN for unparseable input", () => {
    expect(parseEndTime("not-a-time")).toBeNaN();
  });
});

describe("parseWallClockUTC", () => {
  it("interprets a bare wall-clock string as UTC", () => {
    expect(parseWallClockUTC("2023-11-14 22:13:20")).toBe(1700000000000);
    expect(parseWallClockUTC("2023-11-14T22:13:20")).toBe(1700000000000);
  });

  it("honors an explicit trailing Z", () => {
    expect(parseWallClockUTC("2023-11-14T22:13:20Z")).toBe(1700000000000);
  });

  it("honors an explicit numeric offset", () => {
    expect(parseWallClockUTC("2023-11-14T23:13:20+01:00")).toBe(1700000000000);
  });

  it("returns NaN for unparseable input", () => {
    expect(parseWallClockUTC("not-a-time")).toBeNaN();
  });
});

describe("autoStepSeconds", () => {
  it("targets 250 data points for the range", () => {
    expect(autoStepSeconds(2500)).toBe(10);
  });

  it("floors the step at one second", () => {
    expect(autoStepSeconds(100)).toBe(1);
  });

  it("floors fractional results down", () => {
    expect(autoStepSeconds(2592000)).toBe(10368);
  });
});

describe("formatNanos", () => {
  const second = 1_000_000_000;

  it("renders whole seconds", () => {
    expect(formatNanos(15 * second)).toBe("15s");
  });

  it("combines minutes and seconds", () => {
    expect(formatNanos(90 * second)).toBe("1m30s");
  });

  it("combines days, hours, minutes and seconds", () => {
    expect(formatNanos((86400 + 3600 + 60 + 1) * second)).toBe("1d1h1m1s");
  });

  it("renders sub-second durations as milliseconds", () => {
    expect(formatNanos(500_000_000)).toBe("500ms");
  });
});
