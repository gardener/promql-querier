// SPDX-FileCopyrightText: Contributors to the Gardener project
// SPDX-License-Identifier: Apache-2.0

// URL state for the query page. Each query panel is encoded with a positional
// prefix (g0., g1., ...) matching the Prometheus scheme, so a page's full set of
// queries survives a copy-paste of the address bar.

export type PanelType = "instant" | "range";

export interface PanelState {
  // A stable identifier used only as a React key; not serialized.
  id: string;
  type: PanelType;
  expr: string;
  time: string;
  range: string;
  end: string;
  step: string;
  stacked: boolean;
}

let idCounter = 0;

function nextId(): string {
  idCounter += 1;
  return `panel-${idCounter}`;
}

export function newPanel(): PanelState {
  return {
    id: nextId(),
    type: "instant",
    expr: "",
    time: "",
    range: "1h",
    end: "",
    step: "",
    stacked: false,
  };
}

const SUFFIXES = {
  tab: "tab",
  expr: "expr",
  range: "range_input",
  end: "end_input",
  step: "step_input",
  stacked: "stacked",
} as const;

// serializePanels renders the panels as a URL hash fragment. Empty fields are
// omitted to keep the address readable.
export function serializePanels(panels: PanelState[]): string {
  const params = new URLSearchParams();
  panels.forEach((p, i) => {
    const g = `g${i}.`;
    params.set(g + SUFFIXES.tab, p.type);
    if (p.expr !== "") {
      params.set(g + SUFFIXES.expr, p.expr);
    }
    if (p.type === "instant") {
      if (p.time !== "") {
        params.set(g + SUFFIXES.end, p.time);
      }
    } else {
      params.set(g + SUFFIXES.range, p.range);
      if (p.end !== "") {
        params.set(g + SUFFIXES.end, p.end);
      }
      if (p.step !== "") {
        params.set(g + SUFFIXES.step, p.step);
      }
      if (p.stacked) {
        params.set(g + SUFFIXES.stacked, "1");
      }
    }
  });
  return params.toString();
}

// parsePanels reconstructs panels from a URL hash fragment. A missing or empty
// hash yields a single blank panel so the page always has something to render.
export function parsePanels(hash: string): PanelState[] {
  const params = new URLSearchParams(hash.replace(/^#/, ""));
  const byIndex = new Map<number, PanelState>();

  for (const key of params.keys()) {
    const match = key.match(/^g(\d+)\.(.+)$/);
    if (!match) {
      continue;
    }
    const index = parseInt(match[1], 10);
    const suffix = match[2];
    if (!byIndex.has(index)) {
      byIndex.set(index, newPanel());
    }
    const panel = byIndex.get(index)!;
    const value = params.get(key) ?? "";
    switch (suffix) {
      case SUFFIXES.tab:
        panel.type = value === "range" ? "range" : "instant";
        break;
      case SUFFIXES.expr:
        panel.expr = value;
        break;
      case SUFFIXES.range:
        panel.range = value;
        break;
      case SUFFIXES.end:
        // end_input carries the evaluation time for instant panels and the end
        // time for range panels; resolve which after the type is known below.
        panel.end = value;
        panel.time = value;
        break;
      case SUFFIXES.step:
        panel.step = value;
        break;
      case SUFFIXES.stacked:
        panel.stacked = value === "1";
        break;
    }
  }

  if (byIndex.size === 0) {
    return [newPanel()];
  }

  return [...byIndex.entries()]
    .sort((a, b) => a[0] - b[0])
    .map(([, panel]) => {
      // Keep only the field that applies to the panel's resolved type so a later
      // re-serialization does not leak an instant time into a range panel.
      if (panel.type === "instant") {
        panel.end = "";
      } else {
        panel.time = "";
      }
      return panel;
    });
}
