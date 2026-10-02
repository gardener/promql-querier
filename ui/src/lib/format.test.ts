// SPDX-FileCopyrightText: Contributors to the Gardener project
// SPDX-License-Identifier: Apache-2.0

import { describe, expect, it } from "vitest";
import { humanize, labelSetToString, metricNameOf } from "./format";

describe("metricNameOf", () => {
  it("returns the __name__ label", () => {
    expect(metricNameOf({ __name__: "up" })).toBe("up");
  });

  it("returns an empty string when unnamed", () => {
    expect(metricNameOf({ job: "api" })).toBe("");
  });
});

describe("labelSetToString", () => {
  it('renders name{k="v"} with labels sorted', () => {
    expect(labelSetToString({ __name__: "http_requests_total", method: "get", job: "api" })).toBe(
      'http_requests_total{job="api", method="get"}',
    );
  });

  it("omits __name__ from the label list", () => {
    expect(labelSetToString({ __name__: "up", instance: "a" })).toBe('up{instance="a"}');
  });

  it("renders an empty brace set for a bare name", () => {
    expect(labelSetToString({ __name__: "up" })).toBe("up{}");
  });

  it("renders a leading brace when unnamed", () => {
    expect(labelSetToString({ job: "api" })).toBe('{job="api"}');
  });
});

describe("humanize", () => {
  it("leaves small numbers untouched", () => {
    expect(humanize(0)).toBe("0");
    expect(humanize(42)).toBe("42");
    expect(humanize(999)).toBe("999");
  });

  it("applies the largest fitting prefix", () => {
    expect(humanize(1000)).toBe("1k");
    expect(humanize(1500)).toBe("1.5k");
    expect(humanize(1_500_000)).toBe("1.5M");
    expect(humanize(2_000_000_000)).toBe("2G");
    expect(humanize(3_000_000_000_000)).toBe("3T");
  });

  it("handles negative values", () => {
    expect(humanize(-1_500_000)).toBe("-1.5M");
  });

  it("applies fractional prefixes below one", () => {
    expect(humanize(0.001)).toBe("1m");
    expect(humanize(0.0000015)).toBe("1.5µ");
  });

  it("rounds to four significant digits", () => {
    expect(humanize(1_234_567)).toBe("1.235M");
  });

  it("passes non-finite values through unchanged", () => {
    expect(humanize(NaN)).toBe("NaN");
    expect(humanize(Infinity)).toBe("Infinity");
  });
});
