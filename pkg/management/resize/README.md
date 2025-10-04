# In-Place Pod Resizing (Phase 1)

This package implements Phase 1 of the in-place pod resizing feature for CloudNativePG, focusing on CPU-only resource changes.

## Overview

The in-place pod resizing feature allows PostgreSQL clusters to dynamically adjust resource allocations without requiring pod recreation or rolling updates, providing faster and less disruptive scaling operations.

## Phase 1 Implementation

Phase 1 focuses on:
- **CPU-only changes**: Only CPU resource modifications are supported for in-place resize
- **Basic decision engine**: Determines whether to use in-place resize or rolling update
- **Policy configuration**: Configurable thresholds and restart policies
- **Pod spec integration**: Automatic configuration of Kubernetes resize policies

## Components

### DecisionEngine (`decision_engine.go`)
The core logic that determines the optimal resize strategy based on:
- Configured resize policy
- Resource change magnitude
- Kubernetes cluster capabilities
- PostgreSQL-specific constraints

### Pod Spec Configuration (`podspec.go`)
Functions to configure Kubernetes pod specifications with appropriate resize policies for the PostgreSQL container.

## Usage

### Configure Resize Policy

```yaml
apiVersion: postgresql.cnpg.io/v1
kind: Cluster
metadata:
  name: postgres-cluster
spec:
  instances: 3
  resources:
    requests:
      memory: "2Gi"
      cpu: "1000m"
    limits:
      memory: "4Gi" 
      cpu: "2000m"
  resourceResizePolicy:
    strategy: Auto
    cpu:
      restartPolicy: NotRequired
      maxIncreasePct: 100
      maxDecreasePct: 50
    memory:
      restartPolicy: RestartContainer
      maxIncreasePct: 50
      maxDecreasePct: 25
    autoStrategyThresholds:
      cpuIncreaseThreshold: 100
      memoryIncreaseThreshold: 50
      memoryDecreaseThreshold: 25
```

### Programmatic Usage

```go
import (
    "github.com/cloudnative-pg/cloudnative-pg/pkg/management/resize"
    apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
)

// Create decision engine
engine := resize.NewDecisionEngine(cluster, oldSpec, newSpec)

// Determine strategy
strategy, err := engine.DetermineStrategy()
if err != nil {
    // Handle error
}

// Configure pod spec with resize policies
resize.UpdatePodSpecForResize(podSpec, cluster)
```

## Resize Strategies

### InPlace
- Modifies running pods without recreation
- Faster scaling with minimal disruption
- Only used when changes are within configured thresholds
- Phase 1: CPU-only changes

### RollingUpdate
- Traditional pod recreation approach
- Used when in-place resize is not feasible
- Provides maximum safety and consistency
- Required for memory changes in Phase 1
## Limitations (Phase 1)

1. **CPU-only changes**: Memory modifications trigger rolling updates
2. **Basic validation**: Limited PostgreSQL-specific checks
3. **No orchestration**: Actual resize execution not implemented
4. **Kubernetes version**: Assumes in-place resize support

## Testing

Run tests with the `resizing` label:

```bash
# Unit tests
go test ./pkg/management/resize/... -v --ginkgo.label-filter="resizing"

# API tests
go test ./api/v1/... -v --ginkgo.label-filter="resizing"

# All resize tests
go test ./... -v --ginkgo.label-filter="resizing"
```

## Future Phases

- **Phase 2**: Memory resize with PostgreSQL configuration updates
- **Phase 3**: Advanced orchestration and replica coordination
- **Phase 4**: Production hardening and comprehensive testing

## Examples

See `examples/resize/basic-resize-demo.go` for a complete demonstration of the resize functionality.
