// SPDX-FileCopyrightText: Contributors to the Gardener project
// SPDX-License-Identifier: Apache-2.0

const UNIT_SECONDS: Record<string, number> = {
  ms: 0.001,
  s: 1,
  m: 60,
  h: 3600,
  d: 86400,
  w: 604800,
  y: 31536000,
};

// parseDurationSeconds accepts durations (300, 1h, 30m, 1d, 1h30m) and
// returns the total number of seconds, or NaN when unparseable.
export function parseDurationSeconds(input: string): number {
  const text = input.trim();
  if (text === "") {
    return NaN;
  }
  if (/^\d+(\.\d+)?$/.test(text)) {
    return parseFloat(text);
  }
  const re = /(\d+)(ms|s|m|h|d|w|y)/g;
  let total = 0;
  let matched = false;
  let m: RegExpExecArray | null;
  while ((m = re.exec(text)) !== null) {
    matched = true;
    total += parseInt(m[1], 10) * UNIT_SECONDS[m[2]];
  }
  return matched ? total : NaN;
}

const HAS_TIMEZONE = /(Z|[+-]\d{2}:?\d{2})$/;

// parseWallClockUTC parses a datetime string as UTC. A string that already
// carries an explicit timezone (trailing Z or numeric offset) is honored as
// written; a bare wall-clock string is interpreted as UTC rather than local
// time. Returns milliseconds since the epoch, or NaN when unparseable.
export function parseWallClockUTC(input: string): number {
  const text = input.trim();
  const withZone = HAS_TIMEZONE.test(text) ? text : `${text}Z`;
  return Date.parse(withZone);
}

// parseEndTime resolves the end-time input to unix seconds. Blank means now.
export function parseEndTime(input: string): number {
  const text = input.trim();
  if (text === "") {
    return Date.now() / 1000;
  }
  if (/^\d+(\.\d+)?$/.test(text)) {
    return parseFloat(text);
  }
  const parsed = parseWallClockUTC(text);
  if (!Number.isNaN(parsed)) {
    return parsed / 1000;
  }
  return NaN;
}

const DEFAULT_DENSITY = 250;

export function autoStepSeconds(rangeSeconds: number): number {
  return Math.max(Math.floor(rangeSeconds / DEFAULT_DENSITY), 1);
}

const NANOS_PER_SECOND = 1_000_000_000;

// formatNanos renders a duration expressed in nanoseconds as a compact string
// built from whole days, hours, minutes and seconds (for example 15s, 1m30s, 2h).
// Sub-second durations fall back to milliseconds.
export function formatNanos(ns: number): string {
  if (ns < NANOS_PER_SECOND) {
    return `${Math.round(ns / 1_000_000)}ms`;
  }
  let seconds = Math.floor(ns / NANOS_PER_SECOND);
  const units: [string, number][] = [
    ["d", 86400],
    ["h", 3600],
    ["m", 60],
    ["s", 1],
  ];
  let out = "";
  for (const [suffix, size] of units) {
    const count = Math.floor(seconds / size);
    if (count > 0) {
      out += `${count}${suffix}`;
      seconds -= count * size;
    }
  }
  return out;
}
