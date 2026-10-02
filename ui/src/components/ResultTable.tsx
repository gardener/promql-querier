// SPDX-FileCopyrightText: Contributors to the Gardener project
// SPDX-License-Identifier: Apache-2.0

import { Table, Text } from "@mantine/core";
import type { QueryData } from "../api/types";
import { labelSetToString } from "../lib/format";

interface ResultTableProps {
  data: QueryData;
}

function EmptyResult() {
  return (
    <Text c="dimmed" size="sm" py="xs">
      Empty query result.
    </Text>
  );
}

// ResultTable renders an instant-query result. scalar and string show a single
// value; a vector shows one row per sample. A matrix (a subquery evaluated at an
// instant) shows every point as "value @ timestamp".
export function ResultTable({ data }: ResultTableProps) {
  if (data.resultType === "scalar" || data.resultType === "string") {
    return (
      <Text size="md" style={{ fontVariantNumeric: "tabular-nums" }}>
        {data.result[1]}
      </Text>
    );
  }

  if (data.resultType === "vector") {
    if (data.result.length === 0) {
      return <EmptyResult />;
    }
    return (
      <Table withTableBorder withColumnBorders striped>
        <Table.Thead>
          <Table.Tr>
            <Table.Th>Element</Table.Th>
            <Table.Th>Value</Table.Th>
          </Table.Tr>
        </Table.Thead>
        <Table.Tbody>
          {data.result.map((sample, i) => (
            <Table.Tr key={i}>
              <Table.Td>{labelSetToString(sample.metric)}</Table.Td>
              <Table.Td style={{ fontVariantNumeric: "tabular-nums" }}>{sample.value[1]}</Table.Td>
            </Table.Tr>
          ))}
        </Table.Tbody>
      </Table>
    );
  }

  // matrix
  if (data.result.length === 0) {
    return <EmptyResult />;
  }
  return (
    <Table withTableBorder withColumnBorders striped>
      <Table.Thead>
        <Table.Tr>
          <Table.Th>Element</Table.Th>
          <Table.Th>Values</Table.Th>
        </Table.Tr>
      </Table.Thead>
      <Table.Tbody>
        {data.result.map((stream, i) => (
          <Table.Tr key={i}>
            <Table.Td>{labelSetToString(stream.metric)}</Table.Td>
            <Table.Td
              style={{
                whiteSpace: "pre",
                fontVariantNumeric: "tabular-nums",
              }}
            >
              {stream.values.map(([ts, v]) => `${v} @ ${ts}`).join("\n")}
            </Table.Td>
          </Table.Tr>
        ))}
      </Table.Tbody>
    </Table>
  );
}
