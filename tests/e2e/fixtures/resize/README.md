# Resize Test Fixtures

This directory contains test fixtures for the CloudNativePG in-place pod resizing functionality (Phase 1).

## Test Fixtures

### cluster-resize-auto.yaml.template
- **Strategy**: Auto
- **Purpose**: Tests automatic strategy selection based on resource change thresholds
- **CPU Policy**: 100% increase, 50% decrease allowed for in-place resize
- **Memory Policy**: 50% increase, 25% decrease allowed for in-place resize
- **Auto Thresholds**: CPU 100%, Memory 50% increase, Memory 25% decrease

### cluster-resize-inplace.yaml.template  
- **Strategy**: InPlace
- **Purpose**: Tests aggressive in-place resize configuration
- **CPU Policy**: 200% increase, 50% decrease allowed
- **Memory Policy**: 100% increase, 50% decrease allowed
- **Note**: Always attempts in-place resize when possible

### cluster-resize-rolling.yaml.template
- **Strategy**: RollingUpdate
- **Purpose**: Tests conservative rolling update approach
- **Behavior**: Always uses rolling updates regardless of change size

### cluster-resize-thresholds.yaml.template
- **Strategy**: Auto with strict thresholds
- **Purpose**: Tests threshold enforcement and edge cases
- **CPU Policy**: 50% increase, 25% decrease (strict limits)
- **Memory Policy**: 25% increase, 10% decrease (very strict limits)
- **Auto Thresholds**: CPU 50%, Memory 25% increase, Memory 10% decrease

## Usage

These fixtures are used by the `pod_resize_test.go` test suite to validate:

1. **Auto Strategy Selection**: Verifies the decision engine chooses the correct strategy
2. **Threshold Enforcement**: Ensures resource changes respect configured limits
3. **Policy Configuration**: Validates pod specs are configured with correct resize policies
4. **Phase 1 Limitations**: Confirms memory changes fall back to rolling updates
5. **Edge Cases**: Tests zero resources, identical resources, and extreme thresholds

## Running Tests

To run the resize tests specifically:

```bash
# Run all resize tests
ginkgo -v --label-filter="resizing" tests/e2e/

# Run resize tests with basic label
ginkgo -v --label-filter="resizing && basic" tests/e2e/

# Run specific test contexts
ginkgo -v --focus="Pod resize functionality" tests/e2e/
```

## Test Coverage

The tests cover:
- ✅ Auto strategy decision making
- ✅ In-place resize configuration
- ✅ Rolling update fallbacks
- ✅ Threshold validation
- ✅ CPU-only resize (Phase 1)
- ✅ Memory change fallbacks (Phase 1)
- ✅ Pod spec resize policy configuration
- ✅ Edge cases and error conditions
- ✅ Decision engine reason messages
