// SPDX-FileCopyrightText: Contributors to the Gardener project
// SPDX-License-Identifier: Apache-2.0

import { useEffect, useRef } from "react";
import uPlot from "uplot";
import "uplot/dist/uPlot.min.css";
import "./graph.css";
import { Text } from "@mantine/core";
import type { SampleStream } from "../api/types";
import { humanize, labelSetToString, metricNameOf } from "../lib/format";

const GRAPH_HEIGHT = 600;

const Y_AXIS_LABEL_FONT = "12px ";
const Y_AXIS_GUTTER_PX = 16;

// yAxis renders the value axis with SI-abbreviated tick labels (1.5M instead of
// 1500000) and grows the axis gutter to fit the widest label, so large numbers
// are shown in full rather than clipped to their last few digits. The tick
// values default to uPlot's own splits; only their rendering is overridden.
function yAxis(): uPlot.Axis {
  return {
    values: (_u, splits) => splits.map(humanize),
    // size measures the widest rendered label with the canvas 2d context so the
    // gutter is wide enough for the abbreviated numbers plus a small margin.
    size: (u, values) => {
      const ctx = u.ctx;
      ctx.font = Y_AXIS_LABEL_FONT + getComputedStyle(u.root).fontFamily;
      const widest = (values ?? []).reduce((max, v) => Math.max(max, ctx.measureText(v).width), 0);
      return Math.ceil(widest) + Y_AXIS_GUTTER_PX;
    },
  };
}

const SERIES_COLORS = [
  "#1f77b4",
  "#ff7f0e",
  "#2ca02c",
  "#d62728",
  "#9467bd",
  "#8c564b",
  "#e377c2",
  "#7f7f7f",
  "#bcbd22",
  "#17becf",
];

interface GraphProps {
  series: SampleStream[];
  stacked: boolean;
}

// formatUTC renders a unix-seconds timestamp as an ISO-like UTC string
// (YYYY-MM-DD HH:MM:SS UTC), so graph readouts never depend on the viewer's
// local time zone.
function formatUTC(unixSeconds: number): string {
  const iso = new Date(unixSeconds * 1000).toISOString();
  return `${iso.slice(0, 10)} ${iso.slice(11, 19)} UTC`;
}

// buildPlotData turns matrix into uPlot's column-oriented layout: a shared,
// sorted timestamp axis plus one aligned value column per series.
function buildPlotData(series: SampleStream[]): uPlot.AlignedData {
  const timestamps = new Set<number>();
  for (const s of series) {
    for (const [t] of s.values) {
      timestamps.add(t);
    }
  }
  const xs = Array.from(timestamps).sort((a, b) => a - b);
  const xIndex = new Map(xs.map((t, i) => [t, i]));
  const columns: (number | null)[][] = [xs];
  for (const s of series) {
    const col: (number | null)[] = new Array(xs.length).fill(null);
    for (const [t, v] of s.values) {
      col[xIndex.get(t)!] = parseFloat(v);
    }
    columns.push(col);
  }
  return columns as uPlot.AlignedData;
}

// tooltipPlugin renders a floating popup that tracks the cursor and shows the
// hovered timestamp plus the value of the single series the cursor is closest
// to.
function tooltipPlugin(series: SampleStream[]): uPlot.Plugin {
  let tooltip: HTMLDivElement | null = null;
  let focusedSeries: number | null = null;

  function show() {
    if (tooltip) {
      tooltip.style.display = "block";
    }
  }
  function hide() {
    if (tooltip) {
      tooltip.style.display = "none";
    }
  }

  // escapeHTML renders text safe for innerHTML, since label keys and values come
  // from downstream metric data and may contain angle brackets or quotes.
  function escapeHTML(text: string): string {
    return text
      .replace(/&/g, "&amp;")
      .replace(/</g, "&lt;")
      .replace(/>/g, "&gt;")
      .replace(/"/g, "&quot;");
  }

  // labelLines renders every label except __name__ as one line per label, in the
  // form key="value", sorted.
  function labelLines(metric: SampleStream["metric"]): string {
    return Object.keys(metric)
      .filter((k) => k !== "__name__")
      .sort()
      .map(
        (k) =>
          `<div class="u-tooltip-label-line">` +
          `<strong>${escapeHTML(k)}</strong>="${escapeHTML(String(metric[k]))}"` +
          `</div>`,
      )
      .join("");
  }

  // focusedRow renders the tooltip body for the currently focused series at the
  // hovered x-index: the metric name and value on the first line, then one line
  // per label. Returns an empty string when no series is focused or its value at
  // that index is absent. The series index is 1-based to match uPlot.
  function focusedRow(u: uPlot, idx: number): string {
    if (focusedSeries == null) {
      return "";
    }
    const value = u.data[focusedSeries][idx];
    if (value == null) {
      return "";
    }
    const seriesIdx = focusedSeries - 1;
    const metric = series[seriesIdx].metric;
    const color = SERIES_COLORS[seriesIdx % SERIES_COLORS.length];
    const name = metricNameOf(metric);
    return (
      `<div class="u-tooltip-value-line">` +
      `<span class="u-tooltip-swatch" style="background:${color}"></span>` +
      `<span>${name ? escapeHTML(name) + ": " : ""}<strong>${value}</strong></span>` +
      `</div>` +
      labelLines(metric)
    );
  }

  return {
    hooks: {
      init: (u) => {
        tooltip = document.createElement("div");
        tooltip.className = "u-tooltip";
        tooltip.style.display = "none";
        u.over.appendChild(tooltip);
        u.over.addEventListener("mouseenter", show);
        u.over.addEventListener("mouseleave", hide);
      },
      // setSeries fires with focus:true when proximity focus changes; seriesIdx
      // is the focused series (1-based) or null when the cursor leaves all lines.
      // The focus flag is a runtime field uPlot's Series type omits.
      setSeries: (_u, seriesIdx, opts) => {
        if ((opts as { focus?: boolean }).focus != null) {
          focusedSeries = seriesIdx;
        }
      },
      setCursor: (u) => {
        if (!tooltip) {
          return;
        }
        const { left, top, idx } = u.cursor;
        if (left === undefined || top === undefined || idx === null || idx === undefined) {
          hide();
          return;
        }
        const ts = u.data[0][idx];
        tooltip.innerHTML =
          `<div class="u-tooltip-time">${formatUTC(ts)}</div>` + focusedRow(u, idx);
        show();
        placeTooltip(tooltip, u.over, left, top);
      },
    },
  };
}

const TOOLTIP_CURSOR_GAP = 12;

function clamp(v: number, min: number, max: number): number {
  return Math.max(min, Math.min(v, max));
}

// placeTooltip positions the tooltip next to the cursor, flipping to the
// opposite side on whichever axis would otherwise run past the far plot edge,
// then clamping into the plot so the popup stays fully visible even when it is
// taller or wider than the plot. Near the right edge it grows leftward, and near
// the bottom edge it grows upward.
function placeTooltip(tooltip: HTMLDivElement, over: HTMLElement, left: number, top: number): void {
  const w = tooltip.offsetWidth;
  const h = tooltip.offsetHeight;
  const flipX = left + TOOLTIP_CURSOR_GAP + w > over.clientWidth;
  const flipY = top + TOOLTIP_CURSOR_GAP + h > over.clientHeight;
  const x = flipX ? left - TOOLTIP_CURSOR_GAP - w : left + TOOLTIP_CURSOR_GAP;
  const y = flipY ? top - TOOLTIP_CURSOR_GAP - h : top + TOOLTIP_CURSOR_GAP;
  tooltip.style.left = `${clamp(x, 0, over.clientWidth - w)}px`;
  tooltip.style.top = `${clamp(y, 0, over.clientHeight - h)}px`;
}

// stackData accumulates value columns so each series is drawn on top of the
// previous ones. Null gaps are treated as zero for stacking purposes. Series for
// which isHidden returns true do not contribute to the running total, so the stack
// re-adjusts to fill the space of hidden series rather than leaving a gap.
function stackData(
  data: uPlot.AlignedData,
  isHidden: (seriesIdx: number) => boolean,
): uPlot.AlignedData {
  const stacked: (number | null)[][] = [data[0] as number[]];
  const len = (data[0] as number[]).length;
  const acc = new Array<number>(len).fill(0);
  for (let s = 1; s < data.length; s++) {
    const src = data[s] as (number | null)[];
    const col: (number | null)[] = new Array(len);
    for (let i = 0; i < len; i++) {
      if (!isHidden(s)) {
        acc[i] += src[i] ?? 0;
      }
      col[i] = acc[i];
    }
    stacked.push(col);
  }
  return stacked as uPlot.AlignedData;
}

// buildBands pairs each visible series with the next visible series above it, so
// the filled region for a layer is bounded by the two neighbouring visible lines.
// Skipping hidden series keeps a layer's fill intact when an adjacent series is
// hidden. seriesCount is the number of value series (excluding the x axis at 0).
function buildBands(seriesCount: number, isHidden: (seriesIdx: number) => boolean): uPlot.Band[] {
  const bands: uPlot.Band[] = [];
  for (let i = 1; i <= seriesCount; i++) {
    if (isHidden(i)) {
      continue;
    }
    let above = -1;
    for (let j = i + 1; j <= seriesCount; j++) {
      if (!isHidden(j)) {
        above = j;
        break;
      }
    }
    if (above !== -1) {
      bands.push({ series: [above, i] });
    }
  }
  return bands;
}

function buildOptions(
  series: SampleStream[],
  width: number,
  stacked: boolean,
  rawData: uPlot.AlignedData,
): uPlot.Options {
  const uSeries: uPlot.Series[] = [{}];
  series.forEach((s, i) => {
    const color = SERIES_COLORS[i % SERIES_COLORS.length];
    uSeries.push({
      label: labelSetToString(s.metric),
      stroke: color,
      fill: stacked ? color + "40" : undefined,
      width: 1.5,
      points: { show: false },
    });
  });

  // isSeriesHidden reads uPlot's own visibility flag so the recomputed stack and
  // bands always match what the legend shows. Series indices are 1-based (index 0
  // is the x axis).
  const isSeriesHidden = (u: uPlot, seriesIdx: number) => u.series[seriesIdx].show === false;

  // When a legend entry is toggled in stacked mode, re-accumulate the stack and
  // rebuild the bands from only the visible series, then push both into the plot.
  // setData and redraw do not re-fire setSeries, so this does not recurse.
  const restackOnToggle: uPlot.Hooks.Defs["setSeries"] = (u, _seriesIdx, opts) => {
    if (opts.show == null) {
      return;
    }
    const restacked = stackData(rawData, (i) => isSeriesHidden(u, i));
    u.delBand(null);
    for (const band of buildBands(series.length, (i) => isSeriesHidden(u, i))) {
      u.addBand(band);
    }
    u.setData(restacked, false);
  };

  return {
    width: width > 0 ? width : 800,
    height: GRAPH_HEIGHT,
    series: uSeries,
    axes: [{}, yAxis()],
    bands: stacked ? buildBands(series.length, () => false) : [],
    scales: { x: { time: true } },
    // Focus the series line nearest the cursor within this many pixels, so the
    // tooltip can show that one series. prox drives the setSeries focus hook the
    // tooltip plugin reads. alpha 1 keeps the non-focused lines and legend at
    // full opacity, so focus only feeds the tooltip and never dims the graph.
    cursor: { focus: { prox: 30 } },
    focus: { alpha: 1 },
    // Render x-axis tick times in UTC rather than the viewer's local zone, so
    // the axis matches the tooltip and the raw sample timestamps.
    tzDate: (ts) => uPlot.tzDate(new Date(ts * 1e3), "Etc/UTC"),
    // The legend is a plain list of series (colour swatch and label only); the
    // per-point values are shown in the cursor tooltip instead.
    legend: { show: true },
    hooks: stacked ? { setSeries: [restackOnToggle] } : {},
    plugins: [tooltipPlugin(series)],
  };
}

export function Graph({ series, stacked }: GraphProps) {
  const containerRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    const container = containerRef.current;
    if (!container || series.length === 0) {
      return;
    }
    const rawData = buildPlotData(series);
    const plotData = stacked ? stackData(rawData, () => false) : rawData;
    const plot = new uPlot(
      buildOptions(series, container.clientWidth, stacked, rawData),
      plotData,
      container,
    );
    const observer = new ResizeObserver(() => {
      plot.setSize({ width: container.clientWidth || 800, height: GRAPH_HEIGHT });
    });
    observer.observe(container);
    return () => {
      observer.disconnect();
      plot.destroy();
    };
  }, [series, stacked]);

  if (series.length === 0) {
    return (
      <Text c="dimmed" size="sm" py="xs">
        Empty query result.
      </Text>
    );
  }

  return <div ref={containerRef} style={{ width: "100%" }} />;
}
