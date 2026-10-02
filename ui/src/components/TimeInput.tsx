// SPDX-FileCopyrightText: Contributors to the Gardener project
// SPDX-License-Identifier: Apache-2.0

import { DateTimePicker } from "@mantine/dates";
import { parseWallClockUTC } from "../lib/step";

interface TimeInputProps {
  label: string;
  // The stored value is a raw string: empty means "now", otherwise unix seconds
  // or an RFC3339 string typed by the user. The picker drives it to unix seconds.
  value: string;
  onChange: (value: string) => void;
  width?: number;
}

// The Mantine picker renders and reports a Date using its local components. To
// keep the field on UTC, we translate at both boundaries: the stored instant is
// shifted so the picker's local components read as the UTC wall-clock, and the
// picked local components are reinterpreted as UTC when converting back.
function instantToPickerDate(seconds: number): Date {
  const utc = new Date(seconds * 1000);
  return new Date(
    utc.getUTCFullYear(),
    utc.getUTCMonth(),
    utc.getUTCDate(),
    utc.getUTCHours(),
    utc.getUTCMinutes(),
    utc.getUTCSeconds(),
  );
}

function pickerDateToSeconds(date: Date): number {
  const millis = Date.UTC(
    date.getFullYear(),
    date.getMonth(),
    date.getDate(),
    date.getHours(),
    date.getMinutes(),
    date.getSeconds(),
  );
  return Math.floor(millis / 1000);
}

function toDate(value: string): Date | null {
  const text = value.trim();
  if (text === "") {
    return null;
  }
  if (/^\d+(\.\d+)?$/.test(text)) {
    return instantToPickerDate(parseFloat(text));
  }
  const parsed = parseWallClockUTC(text);
  return Number.isNaN(parsed) ? null : instantToPickerDate(parsed / 1000);
}

// TimeInput is a calendar-backed time selector. An empty selection means "now";
// a picked date is stored as unix seconds so the existing query code can forward
// it unchanged.
export function TimeInput({ label, value, onChange, width = 250 }: TimeInputProps) {
  return (
    <DateTimePicker
      label={label}
      w={width}
      clearable
      withSeconds
      valueFormat="YYYY-MM-DD HH:mm:ss"
      placeholder="now"
      value={toDate(value)}
      onChange={(next) => {
        if (!next) {
          onChange("");
          return;
        }
        const date = typeof next === "string" ? new Date(next) : next;
        onChange(String(pickerDateToSeconds(date)));
      }}
    />
  );
}
