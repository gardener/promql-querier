// SPDX-FileCopyrightText: Contributors to the Gardener project
// SPDX-License-Identifier: Apache-2.0

import { useState } from "react";
import {
  ActionIcon,
  Button,
  Group,
  Paper,
  SegmentedControl,
  Stack,
  Tabs,
  TextInput,
} from "@mantine/core";
import { IconX } from "@tabler/icons-react";
import { QueryEditor } from "./QueryEditor";
import { RangeInput } from "./RangeInput";
import { TimeInput } from "./TimeInput";
import { Messages } from "./Messages";
import { ResultTable } from "./ResultTable";
import { Graph } from "./Graph";
import { PlanView } from "./PlanView";
import { instantQuery, planInstant, planRange, rangeQuery } from "../api/client";
import { ApiError } from "../api/types";
import type { SampleStream, PlanDescription, QueryData } from "../api/types";
import { autoStepSeconds, parseDurationSeconds, parseEndTime } from "../lib/step";
import type { PanelState, PanelType } from "../lib/urlState";

interface QueryPanelProps {
  panel: PanelState;
  onChange: (next: Partial<PanelState>) => void;
  onExecute: () => void;
  onRemove?: () => void;
}

export function QueryPanel({ panel, onChange, onExecute, onRemove }: QueryPanelProps) {
  const [data, setData] = useState<QueryData | null>(null);
  const [series, setSeries] = useState<SampleStream[] | null>(null);
  const [plan, setPlan] = useState<PlanDescription | null>(null);
  const [warnings, setWarnings] = useState<string[]>([]);
  const [error, setError] = useState<string | null>(null);
  const [errorType, setErrorType] = useState<string>("");
  const [loading, setLoading] = useState(false);

  function resolvedStepSeconds(rangeSeconds: number): number {
    const manual = panel.step.trim();
    if (manual !== "") {
      const parsed = parseDurationSeconds(manual);
      if (!Number.isNaN(parsed) && parsed > 0) {
        return parsed;
      }
    }
    return autoStepSeconds(rangeSeconds);
  }

  function clearResults() {
    setData(null);
    setSeries(null);
    setPlan(null);
    setWarnings([]);
  }

  async function runInstant(expr: string) {
    const result = await instantQuery(expr, panel.time.trim() || undefined);
    setData(result.data);
    setSeries(null);
    setPlan(null);
    setWarnings(result.warnings);
  }

  // rangeBounds resolves the range tab inputs (range, end, step) into the
  // start/end/step seconds shared by the range query and range plan requests.
  function rangeBounds(): { start: number; end: number; step: number } {
    const rangeSeconds = parseDurationSeconds(panel.range);
    if (Number.isNaN(rangeSeconds) || rangeSeconds <= 0) {
      throw new Error("invalid range duration");
    }
    const endSeconds = parseEndTime(panel.end);
    if (Number.isNaN(endSeconds)) {
      throw new Error("invalid end time");
    }
    return {
      start: endSeconds - rangeSeconds,
      end: endSeconds,
      step: resolvedStepSeconds(rangeSeconds),
    };
  }

  async function runRange(expr: string) {
    const { start, end, step } = rangeBounds();
    const result = await rangeQuery(expr, start, end, step);
    if (result.data.resultType !== "matrix") {
      throw new Error(`range query returned ${result.data.resultType}, expected matrix`);
    }
    setSeries(result.data.result);
    setData(null);
    setPlan(null);
    setWarnings(result.warnings);
  }

  function showPlanResult(result: PlanDescription) {
    setPlan(result);
    setData(null);
    setSeries(null);
    setWarnings([]);
  }

  async function runPlanInstant(expr: string) {
    showPlanResult(await planInstant(expr, panel.time.trim() || undefined));
  }

  async function runPlanRange(expr: string) {
    const { start, end, step } = rangeBounds();
    showPlanResult(await planRange(expr, start, end, step));
  }

  async function dispatch(
    range: (expr: string) => Promise<void>,
    instant: (expr: string) => Promise<void>,
  ) {
    setError(null);
    setErrorType("");
    onExecute();
    const expr = panel.expr.trim();
    if (expr === "") {
      clearResults();
      return;
    }
    setLoading(true);
    try {
      if (panel.type === "range") {
        await range(expr);
      } else {
        await instant(expr);
      }
    } catch (err) {
      clearResults();
      if (err instanceof ApiError) {
        setError(err.message);
        setErrorType(err.errorType);
      } else {
        setError(err instanceof Error ? err.message : String(err));
        setErrorType("");
      }
    } finally {
      setLoading(false);
    }
  }

  function execute() {
    return dispatch(runRange, runInstant);
  }

  function showPlan() {
    return dispatch(runPlanRange, runPlanInstant);
  }

  return (
    <Paper withBorder p="md" radius="sm">
      <Stack gap="sm">
        <Group justify="space-between" align="center">
          <Tabs value={panel.type} onChange={(v) => v && onChange({ type: v as PanelType })}>
            <Tabs.List>
              <Tabs.Tab value="instant">Instant</Tabs.Tab>
              <Tabs.Tab value="range">Range</Tabs.Tab>
            </Tabs.List>
          </Tabs>
          {onRemove && (
            <ActionIcon variant="subtle" color="gray" aria-label="Remove query" onClick={onRemove}>
              <IconX style={{ width: "70%", height: "70%" }} />
            </ActionIcon>
          )}
        </Group>

        <Group align="flex-start" wrap="nowrap" gap="xs">
          <QueryEditor
            key={panel.id}
            initialValue={panel.expr}
            onChange={(expr) => onChange({ expr })}
            onExecute={execute}
          />
          <Button
            variant="light"
            color="thyme"
            onClick={showPlan}
            loading={loading}
            style={{ flexShrink: 0 }}
          >
            Plan
          </Button>
          <Button onClick={execute} loading={loading} style={{ flexShrink: 0 }}>
            Execute
          </Button>
        </Group>

        {panel.type === "instant" ? (
          <Group align="flex-end" gap="md">
            <TimeInput
              label="Evaluation time"
              value={panel.time}
              onChange={(time) => onChange({ time })}
            />
          </Group>
        ) : (
          <Group align="flex-end" gap="md" justify="space-between" wrap="nowrap">
            <Group align="flex-end" gap="md">
              <RangeInput value={panel.range} onChange={(range) => onChange({ range })} />
              <TimeInput label="End time" value={panel.end} onChange={(end) => onChange({ end })} />
              <TextInput
                label="Res. (s)"
                placeholder="auto"
                value={panel.step}
                onChange={(e) => onChange({ step: e.currentTarget.value })}
                w={100}
              />
            </Group>
            <SegmentedControl
              size="sm"
              data={[
                { label: "Stacked", value: "stacked" },
                { label: "Unstacked", value: "unstacked" },
              ]}
              value={panel.stacked ? "stacked" : "unstacked"}
              onChange={(v) => onChange({ stacked: v === "stacked" })}
              color="gardener"
            />
          </Group>
        )}

        <Messages error={error} errorType={errorType} warnings={warnings} />
        {plan && <PlanView plan={plan} />}
        {panel.type === "instant" && data && <ResultTable data={data} />}
        {panel.type === "range" && (
          <div style={{ width: "100%" }}>
            {series && <Graph series={series} stacked={panel.stacked} />}
          </div>
        )}
      </Stack>
    </Paper>
  );
}
