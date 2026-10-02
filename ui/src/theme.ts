// SPDX-FileCopyrightText: Contributors to the Gardener project
// SPDX-License-Identifier: Apache-2.0

import { createTheme, type MantineColorsTuple } from "@mantine/core";

export const BANNER_GREY = "#2b2f33";
export const BANNER_TEXT = "#e8eaed";
export const LAVENDER = "#9b7fc7";

const gardener: MantineColorsTuple = [
  "#eef5f2",
  "#dce9e3",
  "#b6d2c6",
  "#8cbaa6",
  "#69a68c",
  "#4f977a",
  "#2d7e60",
  "#0a6b51",
  "#065e47",
  "#044f3b",
];

const thyme: MantineColorsTuple = [
  "#eef4ef",
  "#dbe6de",
  "#b6ccbd",
  "#8fb199",
  "#6f9a7b",
  "#5a8c68",
  "#4e845d",
  "#3f6f4c",
  "#356342",
  "#1f3d2b",
];

export const theme = createTheme({
  primaryColor: "gardener",
  primaryShade: 7,
  defaultRadius: "sm",
  colors: {
    gardener,
    thyme,
  },
  fontFamily: '-apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, Helvetica, Arial, sans-serif',
});
