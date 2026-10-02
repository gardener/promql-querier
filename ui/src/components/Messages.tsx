// SPDX-FileCopyrightText: Contributors to the Gardener project
// SPDX-License-Identifier: Apache-2.0

import { Alert, List, Stack } from "@mantine/core";

interface MessagesProps {
  error: string | null;
  errorType: string;
  warnings: string[];
}

function toLines(values: string[]): string[] {
  return values.flatMap((value) => value.split("\n")).filter((line) => line.trim() !== "");
}

function ErrorBody({ values }: { values: string[] }) {
  const lines = toLines(values);
  return (
    <List size="sm" spacing={4}>
      {lines.map((line, i) => (
        <List.Item key={i}>{line}</List.Item>
      ))}
    </List>
  );
}

function errorTitle(errorType: string): string {
  switch (errorType) {
    case "bad_data":
      return "Bad request";
    case "execution":
      return "Execution error";
    case "internal":
      return "Internal error";
    default:
      return "Error";
  }
}

export function Messages({ error, errorType, warnings }: MessagesProps) {
  if (!error && warnings.length === 0) {
    return null;
  }
  return (
    <Stack gap="xs" mt="sm">
      {error && (
        <Alert color="red" variant="light" title={errorTitle(errorType)}>
          <ErrorBody values={[error]} />
        </Alert>
      )}
      {warnings.length > 0 && (
        <Alert color="yellow" variant="light" title="Warning">
          <ErrorBody values={warnings} />
        </Alert>
      )}
    </Stack>
  );
}
