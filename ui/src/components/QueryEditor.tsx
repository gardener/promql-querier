// SPDX-FileCopyrightText: Contributors to the Gardener project
// SPDX-License-Identifier: Apache-2.0

import { useEffect, useRef } from "react";
import { Decoration, EditorView, keymap, ViewPlugin, WidgetType } from "@codemirror/view";
import { EditorState, Prec } from "@codemirror/state";
import {
  defaultKeymap,
  history,
  historyKeymap,
  insertNewlineAndIndent,
} from "@codemirror/commands";
import { autocompletion, completionKeymap } from "@codemirror/autocomplete";
import { syntaxHighlighting, HighlightStyle } from "@codemirror/language";
import { tags } from "@lezer/highlight";
import { PromQLExtension } from "@prometheus-io/codemirror-promql";
import { LAVENDER } from "../theme";

interface QueryEditorProps {
  onChange: (value: string) => void;
  onExecute: () => void;
  initialValue?: string;
}

class PlaceholderWidget extends WidgetType {
  constructor(private readonly text: string) {
    super();
  }
  toDOM() {
    const span = document.createElement("span");
    span.className = "cm-placeholder";
    span.textContent = this.text;
    span.style.pointerEvents = "none";
    return span;
  }
  ignoreEvent() {
    return true;
  }
}

function placeholderOnBlur(text: string) {
  return ViewPlugin.fromClass(
    class {
      view: EditorView;
      placeholder: typeof Decoration.none;
      constructor(view: EditorView) {
        this.view = view;
        this.placeholder = Decoration.set([
          Decoration.widget({
            widget: new PlaceholderWidget(text),
            side: 1,
          }).range(0),
        ]);
      }
      get decorations() {
        if (this.view.state.doc.length > 0 || this.view.hasFocus) {
          return Decoration.none;
        }
        return this.placeholder;
      }
    },
    { decorations: (v) => v.decorations },
  );
}

const editorTheme = EditorView.theme({
  "&": {
    fontSize: "14px",
  },
  "&.cm-focused": {
    outline: "none",
  },
  ".cm-scroller": {
    lineHeight: "1.6",
  },
  ".cm-content": {
    fontFamily: "ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace",
    padding: "6px 12px",
  },
  ".cm-line": {
    padding: "0",
  },
  ".cm-cursor": {
    borderLeftWidth: "2px",
  },
  ".cm-placeholder": {
    fontFamily:
      '-apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, Helvetica, Arial, sans-serif',
    color: "var(--mantine-color-gray-5)",
  },
});

const promqlHighlightStyle = HighlightStyle.define([
  { tag: [tags.keyword, tags.operatorKeyword, tags.modifier], color: LAVENDER },
  { tag: tags.function(tags.variableName), color: "#2e7d5b" },
  { tag: tags.string, color: "#b5651d" },
  { tag: tags.number, color: "#b58900" },
  { tag: [tags.operator, tags.logicOperator, tags.arithmeticOperator], color: "#546e7a" },
  { tag: tags.comment, color: "#90a4ae", fontStyle: "italic" },
  { tag: tags.labelName, color: "#5c6bc0" },
  { tag: tags.variableName, color: "#37474f" },
]);

// buildExtensions assembles the CodeMirror setup that gives the query box PromQL
// syntax highlighting and same-origin autocomplete. setComplete with an empty
// remote URL points completion at PromQL Querier's own /api/v1 metadata endpoints.
// autocompletion() must be present for the promql package's completion source,
// which it registers as language data, to fire.
//
// Plain Enter executes the query and Shift+Enter inserts a newline. These bindings
// sit at highest precedence so they win over the default keymap.
function buildExtensions(onChange: (value: string) => void, onExecute: () => void) {
  const promql = new PromQLExtension().setComplete({ remote: { url: "" } });
  const executeKeymap = Prec.highest(
    keymap.of([
      {
        key: "Enter",
        run: () => {
          onExecute();
          return true;
        },
      },
      {
        key: "Shift-Enter",
        run: insertNewlineAndIndent,
      },
    ]),
  );
  return [
    editorTheme,
    syntaxHighlighting(promqlHighlightStyle),
    history(),
    keymap.of([...defaultKeymap, ...historyKeymap, ...completionKeymap]),
    placeholderOnBlur("Expression (press Enter to execute, Shift+Enter for newline)"),
    EditorView.lineWrapping,
    autocompletion(),
    executeKeymap,
    promql.asExtension(),
    EditorView.updateListener.of((update) => {
      if (update.docChanged) {
        onChange(update.state.doc.toString());
      }
    }),
  ];
}

export function QueryEditor({ onChange, onExecute, initialValue = "" }: QueryEditorProps) {
  const parentRef = useRef<HTMLDivElement>(null);
  // Keep the latest callbacks in a ref so the editor is created once and never
  // torn down when the parent re-renders with new closures.
  const handlers = useRef({ onChange, onExecute });
  handlers.current = { onChange, onExecute };
  // The initial document is captured once; later prop changes do not rewrite the
  // editor, since the user is editing it live.
  const initialDoc = useRef(initialValue);

  useEffect(() => {
    if (!parentRef.current) {
      return;
    }
    const view = new EditorView({
      state: EditorState.create({
        doc: initialDoc.current,
        extensions: buildExtensions(
          (value) => handlers.current.onChange(value),
          () => handlers.current.onExecute(),
        ),
      }),
      parent: parentRef.current,
    });
    return () => view.destroy();
  }, []);

  return (
    <div
      ref={parentRef}
      style={{
        flexGrow: 1,
        minWidth: 0,
        border: "1px solid var(--mantine-color-gray-4)",
        borderRadius: "var(--mantine-radius-sm)",
        minHeight: 38,
      }}
    />
  );
}
