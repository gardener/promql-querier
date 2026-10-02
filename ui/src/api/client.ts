// SPDX-FileCopyrightText: Contributors to the Gardener project
// SPDX-License-Identifier: Apache-2.0

import type { ApiResponse, PlanDescription, QueryData, QueryResult, Config } from "./types";
import { ApiError } from "./types";

// Envelope carries a decoded API payload alongside any server warnings.
interface Envelope<T> {
  data: T;
  warnings: string[];
}

const QUERY_LIMIT = "1000";

// postEnvelope sends a form-encoded POST to a PromQL Querier API endpoint and unwraps
// the envelope, turning an error status or non-2xx response into a thrown Error
// carrying the server's message.
async function postEnvelope<T>(path: string, params: Record<string, string>): Promise<Envelope<T>> {
  const body = new URLSearchParams(params);
  const resp = await fetch(path, {
    method: "POST",
    headers: { "Content-Type": "application/x-www-form-urlencoded" },
    body,
  });

  let payload: ApiResponse<T>;
  try {
    payload = (await resp.json()) as ApiResponse<T>;
  } catch {
    throw new Error(`invalid response from server (HTTP ${resp.status})`);
  }

  if (payload.status === "error") {
    throw new ApiError(payload.error || "query failed", payload.errorType ?? "");
  }
  if (!resp.ok) {
    throw new Error(`HTTP ${resp.status}`);
  }
  if (!payload.data) {
    throw new Error("response missing data");
  }

  return { data: payload.data, warnings: payload.warnings ?? [] };
}

// requestData fetches a PromQL Querier API endpoint with GET, unwraps the envelope,
// and returns just its data, turning an error status or non-2xx response into
// a thrown Error carrying the server's message.
async function requestData<T>(path: string): Promise<T> {
  const resp = await fetch(path);

  let payload: ApiResponse<T>;
  try {
    payload = (await resp.json()) as ApiResponse<T>;
  } catch {
    throw new Error(`invalid response from server (HTTP ${resp.status})`);
  }

  if (payload.status === "error") {
    throw new ApiError(payload.error || "request failed", payload.errorType ?? "");
  }
  if (!resp.ok) {
    throw new Error(`HTTP ${resp.status}`);
  }
  if (!payload.data) {
    throw new Error("response missing data");
  }

  return payload.data;
}

export function instantQuery(query: string, time?: string): Promise<QueryResult> {
  const params: Record<string, string> = { query, limit: QUERY_LIMIT };
  if (time) {
    params.time = time;
  }
  return postEnvelope<QueryData>("/api/v1/query", params);
}

export function rangeQuery(
  query: string,
  start: number,
  end: number,
  step: number,
): Promise<QueryResult> {
  return postEnvelope<QueryData>("/api/v1/query_range", {
    query,
    start: String(start),
    end: String(end),
    step: String(step),
    limit: QUERY_LIMIT,
  });
}

export function planInstant(query: string, time?: string): Promise<PlanDescription> {
  const params: Record<string, string> = { query };
  if (time) {
    params.time = time;
  }
  return postEnvelope<PlanDescription>("/api/v1/plan", params).then((e) => e.data);
}

export function planRange(
  query: string,
  start: number,
  end: number,
  step: number,
): Promise<PlanDescription> {
  return postEnvelope<PlanDescription>("/api/v1/plan_range", {
    query,
    start: String(start),
    end: String(end),
    step: String(step),
  }).then((e) => e.data);
}

export function statusConfig(): Promise<Config> {
  return requestData<Config>("/api/v1/status/config");
}
