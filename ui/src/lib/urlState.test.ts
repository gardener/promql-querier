// SPDX-FileCopyrightText: Contributors to the Gardener project
// SPDX-License-Identifier: Apache-2.0

import { describe, expect, it } from "vitest";
import { newPanel, parsePanels, serializePanels, type PanelState } from "./urlState";

// stripId drops the non-serialized React key so round-trip comparisons only
// consider the fields the URL actually carries.
function stripId(panel: PanelState): Omit<PanelState, "id"> {
  const { id: _id, ...rest } = panel;
  return rest;
}

describe("parsePanels", () => {
  it("returns a single blank panel for an empty hash", () => {
    const panels = parsePanels("");
    expect(panels).toHaveLength(1);
    expect(stripId(panels[0])).toEqual(stripId(newPanel()));
  });

  it("ignores a leading hash character", () => {
    const panels = parsePanels("#g0.tab=instant&g0.expr=up");
    expect(panels).toHaveLength(1);
    expect(panels[0].expr).toBe("up");
  });

  it("orders panels by their positional index", () => {
    const panels = parsePanels("g1.tab=instant&g1.expr=b&g0.tab=instant&g0.expr=a");
    expect(panels.map((p) => p.expr)).toEqual(["a", "b"]);
  });

  it("keeps end_input as evaluation time for instant panels", () => {
    const [panel] = parsePanels("g0.tab=instant&g0.end_input=1700000000");
    expect(panel.time).toBe("1700000000");
    expect(panel.end).toBe("");
  });

  it("keeps end_input as end time for range panels", () => {
    const [panel] = parsePanels("g0.tab=range&g0.range_input=6h&g0.end_input=1700000000");
    expect(panel.end).toBe("1700000000");
    expect(panel.time).toBe("");
    expect(panel.range).toBe("6h");
  });
});

describe("serializePanels", () => {
  it("omits empty fields", () => {
    const hash = serializePanels([{ ...newPanel(), type: "instant", expr: "" }]);
    expect(hash).toBe("g0.tab=instant");
  });

  it("only emits range fields for range panels", () => {
    const hash = serializePanels([
      { ...newPanel(), type: "range", expr: "up", range: "6h", step: "15" },
    ]);
    const params = new URLSearchParams(hash);
    expect(params.get("g0.tab")).toBe("range");
    expect(params.get("g0.range_input")).toBe("6h");
    expect(params.get("g0.step_input")).toBe("15");
  });
});

describe("serialize/parse round-trip", () => {
  it("preserves an instant panel", () => {
    const original: PanelState = {
      ...newPanel(),
      type: "instant",
      expr: "rate(http_requests_total[5m])",
      time: "1700000000",
    };
    const parsed = parsePanels(serializePanels([original]));
    expect(parsed).toHaveLength(1);
    expect(stripId(parsed[0])).toEqual(stripId(original));
  });

  it("preserves a range panel", () => {
    const original: PanelState = {
      ...newPanel(),
      type: "range",
      expr: "up",
      range: "12h",
      end: "1700000000",
      step: "30",
    };
    const parsed = parsePanels(serializePanels([original]));
    expect(parsed).toHaveLength(1);
    expect(stripId(parsed[0])).toEqual(stripId(original));
  });

  it("preserves multiple mixed panels", () => {
    const originals: PanelState[] = [
      { ...newPanel(), type: "instant", expr: "up", time: "1700000000" },
      { ...newPanel(), type: "range", expr: "rate(x[5m])", range: "3h", step: "10" },
    ];
    const parsed = parsePanels(serializePanels(originals));
    expect(parsed.map(stripId)).toEqual(originals.map(stripId));
  });
});
