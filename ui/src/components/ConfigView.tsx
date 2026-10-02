// SPDX-FileCopyrightText: Contributors to the Gardener project
// SPDX-License-Identifier: Apache-2.0

import { useEffect, useState } from "react";
import { Badge, Group, Loader, Paper, Table, Text } from "@mantine/core";
import { statusConfig } from "../api/client";
import type { Endpoint } from "../api/types";
import { Messages } from "./Messages";

// ConfigView shows the downstream endpoints PromQL Querier routes to,
// each with its virtual labels, as reported by /api/v1/status/config.
export function ConfigView() {
  const [endpoints, setEndpoints] = useState<Endpoint[] | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    // cancelled guards against StrictMode's double-invoked effect and against a
    // resolve arriving after unmount setting state on a gone component.
    let cancelled = false;
    setError(null);
    setLoading(true);
    statusConfig()
      .then((data) => {
        if (!cancelled) {
          setEndpoints(data.endpoints);
        }
      })
      .catch((err) => {
        if (!cancelled) {
          setError(err instanceof Error ? err.message : String(err));
        }
      })
      .finally(() => {
        if (!cancelled) {
          setLoading(false);
        }
      });
    return () => {
      cancelled = true;
    };
  }, []);

  if (loading) {
    return <Loader />;
  }
  if (error) {
    return <Messages error={error} errorType="" warnings={[]} />;
  }
  if (!endpoints || endpoints.length === 0) {
    return <Text c="dimmed">No downstream endpoints configured.</Text>;
  }

  const rows = endpoints.map((endpoint) => (
    <Table.Tr key={endpoint.url}>
      <Table.Td>{endpoint.url}</Table.Td>
      <Table.Td>
        <Group gap="xs">
          {Object.entries(endpoint.labels).map(([name, value]) => (
            <Badge key={name} color="thyme" variant="light">
              {name}="{value}"
            </Badge>
          ))}
        </Group>
      </Table.Td>
    </Table.Tr>
  ));

  return (
    <Paper withBorder p="md" radius="sm">
      <Table striped withTableBorder>
        <Table.Thead>
          <Table.Tr>
            <Table.Th>URL</Table.Th>
            <Table.Th>Labels</Table.Th>
          </Table.Tr>
        </Table.Thead>
        <Table.Tbody>{rows}</Table.Tbody>
      </Table>
    </Paper>
  );
}
