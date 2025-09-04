# In-Place Pod Resizing Support for CloudNativePG

## Executive Summary

This document proposes implementing support for Kubernetes in-place pod resizing in CloudNativePG (CNPG) to enable dynamic resource scaling of PostgreSQL databases without the need for pod restarts or rolling updates. This feature leverages Kubernetes 1.34's [in-place pod resizing capability](https://kubernetes.io/docs/tasks/configure-pod-container/resize-container-resources/) to provide faster, less disruptive resource adjustments for PostgreSQL workloads.

## Background

### Current State in CNPG

Currently, CNPG handles resource changes through rolling updates:
- Changes to `.spec.resources` trigger a rolling update process
- Each PostgreSQL pod is recreated with new resource specifications
- The process starts with replicas and ends with the primary
- This approach ensures consistency but causes temporary service disruption

### Kubernetes In-Place Pod Resizing

Kubernetes 1.34 introduces the ability to resize pod resources without recreation:
- CPU and memory resources can be adjusted in running pods
- Containers can be configured to restart or continue running during resize
- The feature provides faster scaling with minimal disruption
- Supports both resource increases and decreases

## Proposed Implementation

### 1. API Extensions

#### Cluster Resource Configuration

Extend the `ClusterSpec` to include resize policies:

```go
// ClusterSpec defines the desired state of Cluster
type ClusterSpec struct {
    // ... existing fields ...
    
    // ResourceResizePolicy defines how resource changes should be handled
    // +optional
    ResourceResizePolicy *ResourceResizePolicy `json:"resourceResizePolicy,omitempty"`
}

// ResourceResizePolicy defines policies for in-place resource resizing
type ResourceResizePolicy struct {
    // Strategy determines how resource changes are applied
    // +kubebuilder:validation:Enum=InPlace;RollingUpdate;Auto
    // +kubebuilder:default:=Auto
    Strategy ResourceResizeStrategy `json:"strategy,omitempty"`
    
    // CPU resize policy for PostgreSQL containers
    // +optional
    CPU *ContainerResizePolicy `json:"cpu,omitempty"`
    
    // Memory resize policy for PostgreSQL containers  
    // +optional
    Memory *ContainerResizePolicy `json:"memory,omitempty"`
    
    // Thresholds for automatic strategy selection
    // +optional
    AutoStrategyThresholds *AutoStrategyThresholds `json:"autoStrategyThresholds,omitempty"`
}

type ResourceResizeStrategy string

const (
    // InPlace attempts to resize pods in-place when possible
    ResourceResizeStrategyInPlace ResourceResizeStrategy = "InPlace"
    
    // RollingUpdate always uses traditional rolling update approach
    ResourceResizeStrategyRollingUpdate ResourceResizeStrategy = "RollingUpdate"
    
    // Auto selects the best strategy based on the change magnitude and type
    ResourceResizeStrategyAuto ResourceResizeStrategy = "Auto"
)

type ContainerResizePolicy struct {
    // RestartPolicy defines whether the container should restart during resize
    // +kubebuilder:validation:Enum=NotRequired;RestartContainer
    // +kubebuilder:default:=NotRequired
    RestartPolicy ContainerRestartPolicy `json:"restartPolicy,omitempty"`
    
    // MaxIncreasePct defines the maximum percentage increase allowed for in-place resize
    // Changes exceeding this threshold will trigger rolling update
    // +kubebuilder:validation:Minimum=0
    // +kubebuilder:validation:Maximum=1000
    // +optional
    MaxIncreasePct *int32 `json:"maxIncreasePct,omitempty"`
    
    // MaxDecreasePct defines the maximum percentage decrease allowed for in-place resize
    // +kubebuilder:validation:Minimum=0  
    // +kubebuilder:validation:Maximum=100
    // +optional
    MaxDecreasePct *int32 `json:"maxDecreasePct,omitempty"`
}

type ContainerRestartPolicy string

const (
    ContainerRestartPolicyNotRequired   ContainerRestartPolicy = "NotRequired"
    ContainerRestartPolicyRestartContainer ContainerRestartPolicy = "RestartContainer"
)

type AutoStrategyThresholds struct {
    // CPU increase threshold (percentage) above which rolling update is preferred
    // +kubebuilder:default:=100
    CPUIncreaseThreshold int32 `json:"cpuIncreaseThreshold,omitempty"`
    
    // Memory increase threshold (percentage) above which rolling update is preferred  
    // +kubebuilder:default:=50
    MemoryIncreaseThreshold int32 `json:"memoryIncreaseThreshold,omitempty"`
    
    // Memory decrease threshold (percentage) above which rolling update is preferred
    // +kubebuilder:default:=25
    MemoryDecreaseThreshold int32 `json:"memoryDecreaseThreshold,omitempty"`
}
```

#### Status Extensions

Extend cluster status to track resize operations:

```go
type ClusterStatus struct {
    // ... existing fields ...
    
    // ResizeStatus tracks ongoing resize operations
    // +optional
    ResizeStatus *ResizeStatus `json:"resizeStatus,omitempty"`
}

type ResizeStatus struct {
    // Strategy indicates which resize strategy is being used
    Strategy ResourceResizeStrategy `json:"strategy"`
    
    // Phase indicates the current phase of the resize operation
    Phase ResizePhase `json:"phase"`
    
    // StartedAt indicates when the resize operation started
    StartedAt *metav1.Time `json:"startedAt,omitempty"`
    
    // CompletedAt indicates when the resize operation completed
    CompletedAt *metav1.Time `json:"completedAt,omitempty"`
    
    // InstancesStatus tracks resize status for each instance
    InstancesStatus []InstanceResizeStatus `json:"instancesStatus,omitempty"`
    
    // Message provides additional details about the resize operation
    Message string `json:"message,omitempty"`
}

type ResizePhase string

const (
    ResizePhaseInProgress ResizePhase = "InProgress"
    ResizePhasePending    ResizePhase = "Pending"
    ResizePhaseCompleted  ResizePhase = "Completed"
    ResizePhaseFailed     ResizePhase = "Failed"
)

type InstanceResizeStatus struct {
    // Name of the PostgreSQL instance
    Name string `json:"name"`
    
    // Phase of resize for this instance
    Phase ResizePhase `json:"phase"`
    
    // Strategy used for this instance
    Strategy ResourceResizeStrategy `json:"strategy"`
    
    // Message with additional details
    Message string `json:"message,omitempty"`
}
```

### 2. Controller Logic Implementation

#### Resize Decision Engine

```go
// ResizeDecisionEngine determines the optimal resize strategy
type ResizeDecisionEngine struct {
    cluster *apiv1.Cluster
    oldSpec *corev1.ResourceRequirements
    newSpec *corev1.ResourceRequirements
}

func (r *ResizeDecisionEngine) DetermineStrategy() (ResourceResizeStrategy, error) {
    policy := r.cluster.Spec.ResourceResizePolicy
    if policy == nil {
        return ResourceResizeStrategyRollingUpdate, nil
    }
    
    switch policy.Strategy {
    case ResourceResizeStrategyInPlace:
        if r.canResizeInPlace() {
            return ResourceResizeStrategyInPlace, nil
        }
        return ResourceResizeStrategyRollingUpdate, fmt.Errorf("in-place resize not feasible, falling back to rolling update")
        
    case ResourceResizeStrategyRollingUpdate:
        return ResourceResizeStrategyRollingUpdate, nil
        
    case ResourceResizeStrategyAuto:
        return r.selectAutoStrategy(), nil
        
    default:
        return ResourceResizeStrategyRollingUpdate, nil
    }
}

func (r *ResizeDecisionEngine) canResizeInPlace() bool {
    // Check if Kubernetes cluster supports in-place resize
    if !r.isInPlaceResizeSupported() {
        return false
    }
    
    // Check if changes are within configured thresholds
    if !r.isWithinThresholds() {
        return false
    }
    
    // Check PostgreSQL-specific constraints
    return r.isPostgreSQLCompatible()
}

func (r *ResizeDecisionEngine) selectAutoStrategy() ResourceResizeStrategy {
    thresholds := r.cluster.Spec.ResourceResizePolicy.AutoStrategyThresholds
    if thresholds == nil {
        thresholds = &AutoStrategyThresholds{
            CPUIncreaseThreshold:    100,
            MemoryIncreaseThreshold: 50,
            MemoryDecreaseThreshold: 25,
        }
    }
    
    cpuChange := r.calculateResourceChange("cpu")
    memoryChange := r.calculateResourceChange("memory")
    
    // Use rolling update for large changes or memory decreases
    if cpuChange > float64(thresholds.CPUIncreaseThreshold) ||
       memoryChange > float64(thresholds.MemoryIncreaseThreshold) ||
       (memoryChange < 0 && -memoryChange > float64(thresholds.MemoryDecreaseThreshold)) {
        return ResourceResizeStrategyRollingUpdate
    }
    
    if r.canResizeInPlace() {
        return ResourceResizeStrategyInPlace
    }
    
    return ResourceResizeStrategyRollingUpdate
}
```

#### Pod Specification Updates

Extend the pod creation logic to include resize policies:

```go
// UpdatePodSpecForResize configures pod spec with resize policies
func (r *ClusterReconciler) UpdatePodSpecForResize(
    podSpec *corev1.PodSpec, 
    cluster *apiv1.Cluster,
) {
    policy := cluster.Spec.ResourceResizePolicy
    if policy == nil {
        return
    }
    
    // Find PostgreSQL container
    for i := range podSpec.Containers {
        if podSpec.Containers[i].Name == "postgres" {
            container := &podSpec.Containers[i]
            
            // Configure resize policies
            if container.ResizePolicy == nil {
                container.ResizePolicy = []corev1.ContainerResizePolicy{}
            }
            
            // CPU resize policy
            if policy.CPU != nil {
                container.ResizePolicy = append(container.ResizePolicy, 
                    corev1.ContainerResizePolicy{
                        ResourceName:  corev1.ResourceCPU,
                        RestartPolicy: corev1.ContainerRestartPolicy(policy.CPU.RestartPolicy),
                    })
            }
            
            // Memory resize policy  
            if policy.Memory != nil {
                container.ResizePolicy = append(container.ResizePolicy,
                    corev1.ContainerResizePolicy{
                        ResourceName:  corev1.ResourceMemory,
                        RestartPolicy: corev1.ContainerRestartPolicy(policy.Memory.RestartPolicy),
                    })
            }
            
            break
        }
    }
}
```

### 3. Resize Orchestration

#### Primary vs Replica Handling

```go
// ResizeOrchestrator manages the resize process across the cluster
type ResizeOrchestrator struct {
    client     client.Client
    cluster    *apiv1.Cluster
    pods       []corev1.Pod
    strategy   ResourceResizeStrategy
}

func (r *ResizeOrchestrator) ExecuteResize(ctx context.Context) error {
    switch r.strategy {
    case ResourceResizeStrategyInPlace:
        return r.executeInPlaceResize(ctx)
    case ResourceResizeStrategyRollingUpdate:
        return r.executeRollingUpdate(ctx)
    default:
        return fmt.Errorf("unsupported resize strategy: %s", r.strategy)
    }
}

func (r *ResizeOrchestrator) executeInPlaceResize(ctx context.Context) error {
    // 1. Resize replicas first (parallel execution possible)
    replicas := r.getReplicaPods()
    if err := r.resizePodsInPlace(ctx, replicas); err != nil {
        return fmt.Errorf("failed to resize replicas: %w", err)
    }
    
    // 2. Wait for replica resize completion
    if err := r.waitForResizeCompletion(ctx, replicas); err != nil {
        return fmt.Errorf("replica resize did not complete: %w", err)
    }
    
    // 3. Resize primary last
    primary := r.getPrimaryPod()
    if primary != nil {
        if err := r.resizePodInPlace(ctx, *primary); err != nil {
            return fmt.Errorf("failed to resize primary: %w", err)
        }
        
        if err := r.waitForResizeCompletion(ctx, []corev1.Pod{*primary}); err != nil {
            return fmt.Errorf("primary resize did not complete: %w", err)
        }
    }
    
    return nil
}

func (r *ResizeOrchestrator) resizePodInPlace(ctx context.Context, pod corev1.Pod) error {
    // Update pod spec with new resource requirements using resize subresource
    patch := client.MergeFrom(pod.DeepCopy())
    
    // Update container resources
    for i := range pod.Spec.Containers {
        if pod.Spec.Containers[i].Name == "postgres" {
            pod.Spec.Containers[i].Resources = r.cluster.Spec.Resources
            break
        }
    }
    
    // Use the resize subresource for the patch
    return r.client.SubResource("resize").Patch(ctx, &pod, patch)
}
```

### 4. Monitoring and Observability

#### Metrics Integration

```go
// ResizeMetrics tracks resize operation metrics
type ResizeMetrics struct {
    // Total resize operations
    TotalResizes prometheus.Counter
    
    // Resize duration by strategy
    ResizeDuration *prometheus.HistogramVec
    
    // Resize success rate
    ResizeSuccessRate prometheus.Gauge
    
    // Current resize operations in progress
    ResizesInProgress prometheus.Gauge
}

func (r *ResizeMetrics) RecordResize(strategy string, duration time.Duration, success bool) {
    r.TotalResizes.Inc()
    r.ResizeDuration.WithLabelValues(strategy).Observe(duration.Seconds())
    
    if success {
        r.ResizeSuccessRate.Set(1)
    } else {
        r.ResizeSuccessRate.Set(0)
    }
}
```

#### Event Generation

```go
func (r *ClusterReconciler) recordResizeEvents(cluster *apiv1.Cluster, status ResizeStatus) {
    switch status.Phase {
    case ResizePhaseInProgress:
        r.Recorder.Event(cluster, corev1.EventTypeNormal, "ResizeStarted", 
            fmt.Sprintf("Started %s resize operation", status.Strategy))
            
    case ResizePhaseCompleted:
        duration := status.CompletedAt.Sub(status.StartedAt.Time)
        r.Recorder.Event(cluster, corev1.EventTypeNormal, "ResizeCompleted",
            fmt.Sprintf("Completed %s resize in %v", status.Strategy, duration))
            
    case ResizePhaseFailed:
        r.Recorder.Event(cluster, corev1.EventTypeWarning, "ResizeFailed",
            fmt.Sprintf("Resize operation failed: %s", status.Message))
    }
}
```

## PostgreSQL-Specific Considerations

### 1. Memory Management

PostgreSQL has specific memory management characteristics that affect resize operations:

#### Shared Buffers
- **Challenge**: PostgreSQL allocates shared_buffers at startup
- **Solution**: Configure memory resize policy to `RestartContainer` when shared_buffers changes are significant
- **Threshold**: Restart if memory change exceeds 25% to allow shared_buffers recalculation

#### Work Memory and Maintenance Work Memory
- **Advantage**: These can be adjusted dynamically via `ALTER SYSTEM`
- **Implementation**: Update PostgreSQL configuration after successful memory resize

```go
func (r *PostgreSQLConfigManager) UpdateMemorySettings(ctx context.Context, pod *corev1.Pod, newLimits corev1.ResourceList) error {
    memoryLimit := newLimits[corev1.ResourceMemory]
    
    // Calculate new shared_buffers (25% of total memory)
    sharedBuffers := memoryLimit.Value() / 4
    
    // Calculate work_mem (4MB per connection, assuming 100 max connections)
    workMem := (memoryLimit.Value() - sharedBuffers) / 100 / 4
    
    // Update PostgreSQL configuration
    queries := []string{
        fmt.Sprintf("ALTER SYSTEM SET work_mem = '%dkB'", workMem/1024),
        fmt.Sprintf("ALTER SYSTEM SET maintenance_work_mem = '%dkB'", workMem*4/1024),
        "SELECT pg_reload_conf()",
    }
    
    return r.executeQueries(ctx, pod, queries)
}
```

### 2. Connection Management

#### Active Connections Impact
- **Issue**: Memory decreases might affect active connections
- **Mitigation**: Implement connection draining for significant memory reductions
- **Monitoring**: Track connection count and memory usage during resize

#### PgBouncer Integration
- **Benefit**: Connection pooling can help manage connection impact during resize
- **Implementation**: Coordinate resize with PgBouncer configuration updates

### 3. Replication Considerations

#### Streaming Replication
- **Requirement**: Ensure replicas can handle new resource levels before primary resize
- **Validation**: Check replication lag and connection status post-resize
- **Recovery**: Implement automatic replica rebuild if resize causes replication issues

#### Logical Replication
- **Consideration**: Publication/subscription workloads may be affected by resource changes
- **Monitoring**: Track replication slot lag during resize operations

## Configuration Examples

### Basic In-Place Resize Configuration

```yaml
apiVersion: postgresql.cnpg.io/v1
kind: Cluster
metadata:
  name: postgres-cluster
spec:
  instances: 3
  
  # Resource requirements
  resources:
    requests:
      memory: "2Gi"
      cpu: "1000m"
    limits:
      memory: "4Gi" 
      cpu: "2000m"
  
  # In-place resize configuration
  resourceResizePolicy:
    strategy: Auto
    cpu:
      restartPolicy: NotRequired
      maxIncreasePct: 100
    memory:
      restartPolicy: RestartContainer
      maxIncreasePct: 50
      maxDecreasePct: 25
    autoStrategyThresholds:
      cpuIncreaseThreshold: 100
      memoryIncreaseThreshold: 50
      memoryDecreaseThreshold: 25
```

### Conservative Configuration (Always Rolling Update)

```yaml
apiVersion: postgresql.cnpg.io/v1
kind: Cluster
metadata:
  name: postgres-cluster-conservative
spec:
  instances: 3
  
  resources:
    requests:
      memory: "4Gi"
      cpu: "2000m"
    limits:
      memory: "8Gi"
      cpu: "4000m"
  
  # Always use rolling updates for maximum safety
  resourceResizePolicy:
    strategy: RollingUpdate
```

### Aggressive In-Place Configuration

```yaml
apiVersion: postgresql.cnpg.io/v1
kind: Cluster
metadata:
  name: postgres-cluster-aggressive  
spec:
  instances: 3
  
  resources:
    requests:
      memory: "1Gi"
      cpu: "500m"
    limits:
      memory: "2Gi"
      cpu: "1000m"
  
  # Always attempt in-place resize
  resourceResizePolicy:
    strategy: InPlace
    cpu:
      restartPolicy: NotRequired
      maxIncreasePct: 200
    memory:
      restartPolicy: NotRequired  # Risk: may cause OOM
      maxIncreasePct: 100
      maxDecreasePct: 50
```

## Implementation Phases

### Phase 1: Basic In-Place Resize Support
- **Duration**: 1-2 sprints
- **Scope**:
  - API extensions for resize policies
  - Basic resize decision engine
  - Simple in-place resize for CPU-only changes
  - Unit tests and basic integration tests

### Phase 2: Memory Resize and PostgreSQL Integration
- **Duration**: 2-3 sprints  
- **Scope**:
  - Memory resize with restart policies
  - PostgreSQL configuration updates
  - Connection impact mitigation
  - Enhanced monitoring and metrics

### Phase 3: Advanced Orchestration and Safety
- **Duration**: 2-3 sprints
- **Scope**:
  - Automatic strategy selection
  - Replica coordination and validation
  - Comprehensive error handling and rollback
  - Performance optimization

### Phase 4: Production Hardening
- **Duration**: 1-2 sprints
- **Scope**:
  - Extensive testing with various workloads
  - Documentation and examples
  - Performance benchmarking
  - Security review

## Benefits and Trade-offs

### Benefits

1. **Faster Scaling**: Resource changes without pod recreation
2. **Reduced Downtime**: Minimal service interruption during resize
3. **Better Resource Utilization**: More responsive to workload changes
4. **Operational Efficiency**: Less complex than rolling updates for simple changes
5. **Cost Optimization**: Faster response to cost optimization needs

### Trade-offs

1. **Complexity**: Additional logic for resize decision making
2. **PostgreSQL Constraints**: Memory changes often require restarts anyway
3. **Kubernetes Version Dependency**: Requires Kubernetes 1.34+
4. **Limited Resource Types**: Only CPU and memory supported
5. **Potential Instability**: In-place changes may be less predictable than full recreation

## Risk Mitigation

### 1. Fallback Mechanisms
- Always fall back to rolling update if in-place resize fails
- Implement automatic rollback for failed resize operations
- Provide manual override capabilities

### 2. Validation and Testing
- Pre-resize validation of resource requirements
- Comprehensive test suite covering edge cases
- Canary testing in non-production environments

### 3. Monitoring and Alerting
- Real-time monitoring of resize operations
- Alerts for failed or stuck resize operations
- Performance impact tracking

### 4. Documentation and Training
- Clear guidelines on when to use each strategy
- Troubleshooting guides for common issues
- Best practices for different workload types

## Conclusion

Implementing in-place pod resizing in CloudNativePG will provide significant operational benefits by enabling faster, less disruptive resource scaling for PostgreSQL databases. The proposed implementation balances the advantages of this new Kubernetes feature with the specific requirements and constraints of PostgreSQL workloads.

The phased approach ensures that the feature can be delivered incrementally while maintaining the stability and reliability that CNPG users expect. The comprehensive configuration options allow users to choose the appropriate level of aggressiveness for their specific use cases, from conservative rolling updates to aggressive in-place resizing.

This enhancement positions CloudNativePG as a leader in leveraging cutting-edge Kubernetes features while maintaining focus on PostgreSQL-specific operational excellence.

---

**References:**
- [Kubernetes In-Place Pod Resizing Documentation](https://kubernetes.io/docs/tasks/configure-pod-container/resize-container-resources/)
- [CloudNativePG Rolling Updates Documentation](https://cloudnative-pg.io/documentation/current/rolling_update/)
- [PostgreSQL Memory Management Best Practices](https://www.postgresql.org/docs/current/runtime-config-resource.html)
