// SPDX-FileCopyrightText: Contributors to the Gardener project
// SPDX-License-Identifier: Apache-2.0

import type { MetricLabels } from "../api/types";

export function metricNameOf(metric: MetricLabels): string {
  return metric.__name__ ?? "";
}

// labelSetToString renders a metric as name{k="v", ...} with labels sorted.
export function labelSetToString(metric: MetricLabels): string {
  const name = metricNameOf(metric);
  const inner = Object.keys(metric)
    .filter((k) => k !== "__name__")
    .sort()
    .map((k) => `${k}="${metric[k]}"`)
    .join(", ");
  return `${name}{${inner}}`;
}

// SI_PREFIXES pairs a decimal exponent with its metric symbol, ordered from
// largest to smallest so humanize can pick the first prefix a value clears.
const SI_PREFIXES: ReadonlyArray<{ exp: number; symbol: string }> = [
  { exp: 24, symbol: "Y" },
  { exp: 21, symbol: "Z" },
  { exp: 18, symbol: "E" },
  { exp: 15, symbol: "P" },
  { exp: 12, symbol: "T" },
  { exp: 9, symbol: "G" },
  { exp: 6, symbol: "M" },
  { exp: 3, symbol: "k" },
  { exp: 0, symbol: "" },
  { exp: -3, symbol: "m" },
  { exp: -6, symbol: "µ" },
  { exp: -9, symbol: "n" },
];

// humanize renders a number with an SI metric prefix (k, M, G, T, ...) so
// large axis ticks stay short enough to fit, e.g. 1500000 becomes "1.5M". Values
// are rounded to at most three significant fractional digits and trailing zeros
// are dropped. Non-finite values render through String() unchanged.
export function humanize(value: number): string {
  if (!Number.isFinite(value)) {
    return String(value);
  }
  if (value === 0) {
    return "0";
  }
  const abs = Math.abs(value);
  const prefix =
    SI_PREFIXES.find((p) => abs >= Math.pow(10, p.exp)) ?? SI_PREFIXES[SI_PREFIXES.length - 1];
  const scaled = value / Math.pow(10, prefix.exp);
  const rounded = parseFloat(scaled.toPrecision(4));
  return `${rounded}${prefix.symbol}`;
}
