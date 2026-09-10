// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 NVIDIA Corporation

export interface Karta {
  apiVersion: string;
  kind: string;
  metadata: { name: string };
  spec: {
    structureDefinition: {
      rootComponent: {
        kind?: GroupVersionKind;
      };
    };
  };
}

export interface Workload {
  apiVersion: string;
  kind: string;
  metadata: { name: string; namespace?: string };
  [field: string]: unknown;
}

export interface GroupVersionKind {
  group: string;
  version: string;
  kind: string;
}

export interface Scale {
  replicas?: number;
  minReplicas?: number;
  maxReplicas?: number;
}

export interface WorkloadStatus {
  Phases: string[];
}

export interface ComponentNode {
  Name: string;
  Kind: GroupVersionKind | null;
  HasPodDefinition: boolean;
  Instances: InstanceNode[];
}

export interface InstanceNode {
  InstanceKey: string | null;
  ReplicaKey: string | null;
  Scale: Scale | null;
  ExtractedInstance: unknown;
  Children: ComponentNode[];
}

export interface WorkloadTree {
  Status: WorkloadStatus | null;
  Children: ComponentNode[];
}
