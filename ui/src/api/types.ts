// SPDX-FileCopyrightText: Contributors to the Gardener project
// SPDX-License-Identifier: Apache-2.0

export type MetricLabels = Record<string, string>;

export type SamplePair = [number, string];

export interface Sample {
  metric: MetricLabels;
  value: SamplePair;
}

export interface SampleStream {
  metric: MetricLabels;
  values: SamplePair[];
}

export type VectorResult = {
  resultType: "vector";
  result: Sample[];
};

export type MatrixResult = {
  resultType: "matrix";
  result: SampleStream[];
};

export type ScalarResult = {
  resultType: "scalar";
  result: SamplePair;
};

export type StringResult = {
  resultType: "string";
  result: SamplePair;
};

export type QueryData = VectorResult | MatrixResult | ScalarResult | StringResult;

export interface ApiResponse<T> {
  status: "success" | "error";
  data?: T;
  error?: string;
  errorType?: string;
  warnings?: string[];
}

export class ApiError extends Error {
  errorType: string;

  constructor(message: string, errorType: string) {
    super(message);
    this.errorType = errorType;
  }
}

export interface QueryResult {
  data: QueryData;
  warnings: string[];
}

export interface PartitionDescription {
  expr: string;
  downstream_expr: string;
  downstreams: string[];
  is_range: boolean;
  time?: string;
  start?: string;
  end?: string;
  step?: number;
}

export interface PlanDescription {
  expr: string;
  is_range: boolean;
  time?: string;
  start?: string;
  end?: string;
  step?: number;
  partitions: PartitionDescription[];
}

export interface Endpoint {
  url: string;
  labels: MetricLabels;
}

export interface Config {
  endpoints: Endpoint[];
}
