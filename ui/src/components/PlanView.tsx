// SPDX-FileCopyrightText: Contributors to the Gardener project
// SPDX-License-Identifier: Apache-2.0

import { Code, Group, Input, Paper, Stack, Table, Text } from "@mantine/core";
import type { PlanDescription } from "../api/types";
import { formatNanos } from "../lib/step";

interface PlanViewProps {
  plan: PlanDescription;
}

// LocalField renders one labeled field of the local expression's execution
// mode, laid out as "label: value" so the local expression reads as a small
// record rather than a one-row table.
function LocalField({ label, value }: { label: string; value: string }) {
  return (
    <Group gap="xs" wrap="nowrap">
      <Text fw={700} size="sm" style={{ minWidth: 90 }}>
        {label}:
      </Text>
      <Text size="sm" style={{ whiteSpace: "nowrap" }}>
        {value}
      </Text>
    </Group>
  );
}

// PartitionExpr renders a partition's two expression forms: expr as written,
// then downstreamExpr actually sent (with virtual labels stripped) below it,
// reached by a down-and-right elbow. Both are always shown so the layout
// consistently reads as "query, then what was sent."
function PartitionExpr({ expr, downstreamExpr }: { expr: string; downstreamExpr: string }) {
  return (
    <Stack gap={2}>
      <Code style={{ background: "transparent" }}>{expr}</Code>
      <Group gap={4} wrap="nowrap" pl="md">
        <Text c="dimmed" size="sm" span>
          &#x21B3;
        </Text>
        <Code style={{ background: "transparent" }}>{downstreamExpr}</Code>
      </Group>
    </Stack>
  );
}

// PlanView renders a query plan: the expression evaluated locally on top of the
// merged downstream results together with how it is evaluated, then one row per
// partition showing the pushed-down expression, whether it runs as an instant
// or range query, the downstreams it targets, and its time window. TS is
// populated for instant partitions; Start, End and Step for range partitions.
export function PlanView({ plan }: PlanViewProps) {
  return (
    <Stack gap="sm">
      <Paper withBorder p="sm" radius="sm" bg="var(--mantine-color-body)">
        <Input.Label fw={700} mb={4}>
          Local expression
        </Input.Label>
        <Code block>{plan.expr}</Code>
        <Stack gap={2} mt="xs">
          <LocalField label="type" value={plan.is_range ? "range" : "instant"} />
          {plan.is_range ? (
            <>
              <LocalField label="start" value={plan.start ?? ""} />
              <LocalField label="end" value={plan.end ?? ""} />
              <LocalField label="step" value={plan.step ? formatNanos(plan.step) : ""} />
            </>
          ) : (
            <LocalField label="time" value={plan.time ?? ""} />
          )}
        </Stack>
      </Paper>

      {(plan.partitions ?? []).length === 0 ? (
        <Text c="dimmed" size="sm" py="xs">
          Empty plan.
        </Text>
      ) : (
        <Table withTableBorder withColumnBorders striped>
          <Table.Thead>
            <Table.Tr>
              <Table.Th>#</Table.Th>
              <Table.Th>Expression</Table.Th>
              <Table.Th>Downstreams</Table.Th>
              <Table.Th>Type</Table.Th>
              <Table.Th>Time</Table.Th>
              <Table.Th>Start</Table.Th>
              <Table.Th>End</Table.Th>
              <Table.Th>Step</Table.Th>
            </Table.Tr>
          </Table.Thead>
          <Table.Tbody>
            {(plan.partitions ?? []).map((partition, i) => (
              <Table.Tr key={i}>
                <Table.Td>{i}</Table.Td>
                <Table.Td>
                  <PartitionExpr expr={partition.expr} downstreamExpr={partition.downstream_expr} />
                </Table.Td>
                <Table.Td style={{ whiteSpace: "pre" }}>
                  {partition.downstreams.join("\n")}
                </Table.Td>
                <Table.Td>{partition.is_range ? "range" : "instant"}</Table.Td>
                <Table.Td style={{ whiteSpace: "nowrap" }}>
                  {partition.is_range ? "" : (partition.time ?? "")}
                </Table.Td>
                <Table.Td style={{ whiteSpace: "nowrap" }}>
                  {partition.is_range ? (partition.start ?? "") : ""}
                </Table.Td>
                <Table.Td style={{ whiteSpace: "nowrap" }}>
                  {partition.is_range ? (partition.end ?? "") : ""}
                </Table.Td>
                <Table.Td style={{ whiteSpace: "nowrap" }}>
                  {partition.is_range && partition.step ? formatNanos(partition.step) : ""}
                </Table.Td>
              </Table.Tr>
            ))}
          </Table.Tbody>
        </Table>
      )}
    </Stack>
  );
}
