// SPDX-FileCopyrightText: Contributors to the Gardener project
// SPDX-License-Identifier: Apache-2.0

import { ActionIcon, TextInput } from "@mantine/core";
import { IconMinus, IconPlus } from "@tabler/icons-react";
import { parseDurationSeconds } from "../lib/step";

// rangeSteps is the sequence the +/- buttons snap through: from one
// second up to a year, in seconds.
const rangeSteps = [
  1,
  10,
  60,
  5 * 60,
  15 * 60,
  30 * 60,
  60 * 60,
  2 * 60 * 60,
  6 * 60 * 60,
  12 * 60 * 60,
  24 * 60 * 60,
  48 * 60 * 60,
  7 * 24 * 60 * 60,
  14 * 24 * 60 * 60,
  30 * 24 * 60 * 60,
  90 * 24 * 60 * 60,
  180 * 24 * 60 * 60,
  365 * 24 * 60 * 60,
];

// formatRange renders a number of seconds as a compact duration (for
// example 3600 becomes "1h"), used to fill the input after a +/- click.
function formatRange(seconds: number): string {
  const units: [number, string][] = [
    [365 * 24 * 60 * 60, "y"],
    [7 * 24 * 60 * 60, "w"],
    [24 * 60 * 60, "d"],
    [60 * 60, "h"],
    [60, "m"],
    [1, "s"],
  ];
  for (const [size, suffix] of units) {
    if (seconds >= size && seconds % size === 0) {
      return `${seconds / size}${suffix}`;
    }
  }
  return `${seconds}s`;
}

interface RangeInputProps {
  value: string;
  onChange: (value: string) => void;
}

export function RangeInput({ value, onChange }: RangeInputProps) {
  const seconds = parseDurationSeconds(value);

  function increase() {
    const current = Number.isNaN(seconds) ? 0 : seconds;
    for (const step of rangeSteps) {
      if (current < step) {
        onChange(formatRange(step));
        return;
      }
    }
  }

  function decrease() {
    const current = Number.isNaN(seconds) ? 0 : seconds;
    for (const step of [...rangeSteps].reverse()) {
      if (current > step) {
        onChange(formatRange(step));
        return;
      }
    }
  }

  const iconStyle = { width: "70%", height: "70%" };

  return (
    <TextInput
      label="Range"
      w={160}
      value={value}
      onChange={(e) => onChange(e.currentTarget.value)}
      leftSection={
        <ActionIcon
          size="lg"
          variant="transparent"
          color="gray"
          aria-label="Decrease range"
          onClick={decrease}
        >
          <IconMinus style={iconStyle} />
        </ActionIcon>
      }
      rightSection={
        <ActionIcon
          size="lg"
          variant="transparent"
          color="gray"
          aria-label="Increase range"
          onClick={increase}
        >
          <IconPlus style={iconStyle} />
        </ActionIcon>
      }
    />
  );
}
