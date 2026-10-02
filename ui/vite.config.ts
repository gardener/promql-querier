// SPDX-FileCopyrightText: Contributors to the Gardener project
// SPDX-License-Identifier: Apache-2.0

import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";

// The Go server embeds dist/ and serves it under /static/, while the built HTML
// page is served at /query. Setting base to /static/ makes that page reference
// its hashed assets as /static/assets/index-<hash>.{js,css}, which resolve
// regardless of whether the page is loaded at /query.
//
// The entry template is app.html (not index.html) so the build output lands at
// dist/app.html and never overwrites the committed dist/index.html placeholder.
// The placeholder is what a fresh clone embeds before "make ui" has produced the
// real bundle. For the same reason emptyOutDir stays off (it would delete the
// placeholder); the ui make target cleans the generated files explicitly.
export default defineConfig({
  base: "/static/",
  plugins: [react()],
  build: {
    outDir: "dist",
    emptyOutDir: false,
    rollupOptions: {
      input: "app.html",
      output: {
        manualChunks(id) {
          if (
            id.includes("@mantine/core") ||
            id.includes("@mantine/hooks") ||
            id.includes("@mantine/dates")
          ) {
            return "mantine";
          }
          if (
            id.includes("@codemirror/") ||
            id.includes("@prometheus-io/codemirror-promql")
          ) {
            return "codemirror";
          }
          if (id.includes("uplot")) {
            return "uplot";
          }
        },
      },
    },
  },
});
