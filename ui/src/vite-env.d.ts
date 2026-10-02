// SPDX-FileCopyrightText: Contributors to the Gardener project
// SPDX-License-Identifier: Apache-2.0

/// <reference types="vite/client" />

declare module "*.svg?url" {
  const src: string;
  export default src;
}
