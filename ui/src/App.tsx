// SPDX-FileCopyrightText: Contributors to the Gardener project
// SPDX-License-Identifier: Apache-2.0

import React from "react";
import { Box, Group, Text } from "@mantine/core";
import { QueryController } from "./components/QueryController";
import { ConfigView } from "./components/ConfigView";
import { BANNER_GREY, BANNER_TEXT } from "./theme";
import logoUrl from "./assets/gardener.svg?url";

const navLinkStyle: React.CSSProperties = {
  color: BANNER_TEXT,
  textDecoration: "none",
  padding: "6px 12px",
  borderRadius: 4,
  fontSize: 14,
  fontWeight: 500,
};

const navLinkHoverStyle: React.CSSProperties = {
  backgroundColor: "rgba(255,255,255,0.12)",
};

function NavLink({ href, children }: { href: string; children: React.ReactNode }) {
  const [hovered, setHovered] = React.useState(false);
  return (
    <a
      href={href}
      style={hovered ? { ...navLinkStyle, ...navLinkHoverStyle } : navLinkStyle}
      onMouseEnter={() => setHovered(true)}
      onMouseLeave={() => setHovered(false)}
    >
      {children}
    </a>
  );
}

export function App() {
  const isConfig = window.location.pathname === "/config";
  return (
    <>
      <Box
        component="header"
        bg={BANNER_GREY}
        px="md"
        h={56}
        style={{ display: "flex", alignItems: "center" }}
      >
        <Group gap="xl" align="center">
          <Group gap="xs" align="center">
            <img src={logoUrl} alt="" width={40} height={40} />
            <Text fw={600} fz={20} c={BANNER_TEXT} style={{ letterSpacing: "0.02em" }}>
              Gardener PromQL Querier
            </Text>
          </Group>
          <Group gap={0}>
            <NavLink href="/query">Query</NavLink>
            <NavLink href="/config">Configuration</NavLink>
          </Group>
        </Group>
      </Box>
      <Box px="lg" py="md">
        {isConfig ? <ConfigView /> : <QueryController />}
      </Box>
    </>
  );
}
