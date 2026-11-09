// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

export type MetricExpression =
  | MetricReference
  | RuntimeInput
  | Literal
  | BinaryExpression
  | FunctionExpression;

export interface MetricReference {
  operation: 'metric';
  id: number;
}

export interface RuntimeInput {
  operation: 'runtime-input';
  name: string;
}

export interface Literal {
  operation: 'literal';
  value: number;
}

export interface BinaryExpression {
  operation: 'divide' | 'add' | 'subtract' | 'multiply';
  left: MetricExpression;
  right: MetricExpression;
}

export interface FunctionExpression {
  operation: 'minimum' | 'maximum';
  arguments: MetricExpression[];
}

export interface Metric {
  id: number;
  kind: 'hardware' | 'derived';
  machineName: string;
  layoutName: string;
  humanName?: string;
  unit?: string;
  shortDescription?: string;
  scaleMultiplier?: number;
  expression?: MetricExpression;
}

export interface SelectedMetricGroup {
  id: string;
  name: string;
  description: string;
  metrics: Metric[];
}

export interface MetricGroup {
  longDescription?: string;
  name: string;
  metrics: Metric[];
}

export interface MetricSection {
  name: string;
  groups: MetricGroup[];
}

export interface Product {
  names: string[];
}

export interface MetricSet {
  products: Product[];
  sections: MetricSection[];
}

export interface MetricSelection {
  metricSetId: number;
  selectedGroups: SelectedMetricGroup[];
  selectedMetrics: Metric[];
  metricsById: Map<number, Metric>;
}

export interface MetricPlan extends MetricSelection {
  requiredMetrics: Metric[];
}
