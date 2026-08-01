export interface TargetRef {
  namespace: string;
  name: string;
  kind: 'Deployment' | 'StatefulSet' | 'CronJob';
}

export interface ObservedState {
  replicas: number;
  suspended: boolean;
  powerState: string;
}

export interface RuleReference {
  kind: string;
  name: string;
  namespace: string;
  priority: number;
  description?: string;
}

export interface BlockReason {
  type: string;
  message: string;
  waivable: boolean;
}

export interface Snapshot {
  available: boolean;
  replicaCount?: number;
  suspended?: boolean;
  resources: { cpuMillicores: number; memoryMiB: number };
  capturedAt?: string;
}

export interface Ownership {
  type: string;
  optedIn: boolean;
}

export interface Savings {
  cpuHoursSaved: number;
  memoryGiBHoursSaved: number;
  estimatedCost: number;
}

export interface PowerTarget {
  metadata: { name: string; namespace: string };
  spec: { targetRef: TargetRef };
  status: {
    observedState: ObservedState;
    desiredState: string;
    managed: boolean;
    divergent: boolean;
    winningRule?: RuleReference;
    suppressedRules?: RuleReference[];
    blocked: boolean;
    blockReasons?: BlockReason[];
    snapshot?: Snapshot;
    ownership?: Ownership[];
    savings?: Savings;
    lastTransition?: string;
    lastReconciliation?: string;
  };
}

export interface StatusSummary {
  totalTargets: number;
  poweredOn: number;
  poweredOff: number;
  blocked: number;
  divergent: number;
  activePolicies: number;
  activeOverrides: number;
  estimatedMonthlySavings?: number;
}

export interface DiscoverySummary {
  totalWorkloads: number;
  eligible: number;
  blocked: number;
  estimatedMonthlySavings?: number;
  suggestedPolicy?: {
    namespace: string;
    schedule: string;
    affectedCount: number;
    estimatedSavings: number;
  };
}

export interface SavingsSummary {
  totalCPUHours: number;
  totalMemoryGiB: number;
  totalEstimatedCost: number;
}

export interface AuditEvent {
  spec: {
    timestamp: string;
    action: string;
    actor: string;
    target: TargetRef;
    result: string;
    reason: string;
    ruleName?: string;
  };
}
