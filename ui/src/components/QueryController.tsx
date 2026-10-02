// SPDX-FileCopyrightText: Contributors to the Gardener project
// SPDX-License-Identifier: Apache-2.0

import { useCallback, useEffect, useRef, useState } from "react";
import { Button, Stack } from "@mantine/core";
import { IconPlus } from "@tabler/icons-react";
import { QueryPanel } from "./QueryPanel";
import { newPanel, parsePanels, serializePanels, type PanelState } from "../lib/urlState";

export function QueryController() {
  const [panels, setPanels] = useState<PanelState[]>(() => parsePanels(window.location.hash));

  // Guards against the hashchange listener re-parsing a hash this component just
  // wrote, which would rebuild every panel and remount their editors.
  const selfWrite = useRef(false);

  useEffect(() => {
    const onHashChange = () => {
      if (selfWrite.current) {
        selfWrite.current = false;
        return;
      }
      setPanels(parsePanels(window.location.hash));
    };
    window.addEventListener("hashchange", onHashChange);
    return () => window.removeEventListener("hashchange", onHashChange);
  }, []);

  // writeURL reflects the current panels into the address bar so the page can be
  // copied and reloaded. Called on execute rather than on every keystroke. It
  // reads the latest state through the updater so it never serializes a stale
  // snapshot captured at render time.
  const writeURL = useCallback(() => {
    setPanels((current) => {
      const hash = serializePanels(current);
      if (window.location.hash.replace(/^#/, "") !== hash) {
        selfWrite.current = true;
        window.location.hash = hash;
      }
      return current;
    });
  }, []);

  function updatePanel(index: number, next: Partial<PanelState>) {
    setPanels((prev) => prev.map((p, i) => (i === index ? { ...p, ...next } : p)));
  }

  function addPanel() {
    setPanels((prev) => [...prev, newPanel()]);
  }

  function removePanel(index: number) {
    setPanels((prev) => prev.filter((_, i) => i !== index));
  }

  return (
    <Stack gap="md">
      {panels.map((panel, i) => (
        <QueryPanel
          key={panel.id}
          panel={panel}
          onChange={(next) => updatePanel(i, next)}
          onExecute={() => writeURL()}
          onRemove={panels.length > 1 ? () => removePanel(i) : undefined}
        />
      ))}
      <Button
        variant="light"
        color="thyme"
        leftSection={<IconPlus style={{ width: "1rem", height: "1rem" }} />}
        onClick={addPanel}
        style={{ alignSelf: "flex-start" }}
      >
        Add Query
      </Button>
    </Stack>
  );
}
