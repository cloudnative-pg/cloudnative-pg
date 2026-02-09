/*
Copyright © contributors to CloudNativePG, established as
CloudNativePG a Series of LF Projects, LLC.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.

SPDX-License-Identifier: Apache-2.0
*/

package autoresize

import (
	"fmt"
	"strconv"
	"strings"

	"gopkg.in/inf.v0"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/util/intstr"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
)

const (
	defaultStepPercent = "20%"
	defaultMinStep     = "2Gi"
	defaultMaxStep     = "500Gi"
)

// isPercentageStep checks if the step is specified as a percentage.
func isPercentageStep(step intstr.IntOrString) bool {
	if step.Type != intstr.String {
		return false
	}
	return strings.HasSuffix(step.StrVal, "%")
}

// parsePercentage extracts the percentage value from a string like "20%".
func parsePercentage(step intstr.IntOrString) (int, error) {
	if step.Type != intstr.String {
		return 0, fmt.Errorf("step is not a string type")
	}

	strVal := strings.TrimSuffix(step.StrVal, "%")
	percent, err := strconv.Atoi(strVal)
	if err != nil {
		return 0, fmt.Errorf("failed to parse percentage from '%s': %w", step.StrVal, err)
	}

	if percent < 0 || percent > 100 {
		return 0, fmt.Errorf("percentage out of range: %d", percent)
	}

	return percent, nil
}

// CalculateNewSize computes the new PVC size based on the expansion policy and current size.
func CalculateNewSize(currentSize resource.Quantity, policy *apiv1.ExpansionPolicy) (resource.Quantity, error) {
	if policy == nil {
		return currentSize, fmt.Errorf("expansion policy is nil")
	}

	// Determine the step to use
	stepVal := policy.Step
	// Default step when zero value (either empty string or zero int)
	if (stepVal.Type == intstr.String && stepVal.StrVal == "") ||
		(stepVal.Type == intstr.Int && stepVal.IntVal == 0) {
		stepVal = intstr.FromString(defaultStepPercent)
	}

	normalizedStep, err := normalizeStep(stepVal)
	if err != nil {
		return currentSize, err
	}
	stepVal = normalizedStep

	currentDec := currentSize.AsDec()
	var expansionStepDec *inf.Dec

	//nolint:nestif // logic is straightforward: percentage vs absolute step calculation
	if isPercentageStep(stepVal) {
		expansionStepDec, err = calculatePercentageStep(currentDec, stepVal, policy)
		if err != nil {
			return currentSize, err
		}
	} else {
		// Absolute value step
		qty, err := resource.ParseQuantity(stepVal.StrVal)
		if err != nil {
			return currentSize, fmt.Errorf("failed to parse step as quantity: %w", err)
		}
		expansionStepDec = qty.AsDec()
	}

	// Calculate new size: currentSize + expansionStep
	newSizeDec := new(inf.Dec).Add(currentDec, expansionStepDec)

	// Apply limit if specified
	if policy.Limit != "" {
		limit, err := resource.ParseQuantity(policy.Limit)
		if err != nil {
			return currentSize, fmt.Errorf("failed to parse limit: %w", err)
		}

		limitDec := limit.AsDec()
		if limitDec.Cmp(inf.NewDec(0, 0)) > 0 && newSizeDec.Cmp(limitDec) > 0 {
			newSizeDec = limitDec
		}
	}

	return *resource.NewDecimalQuantity(*newSizeDec, currentSize.Format), nil
}

// calculatePercentageStep computes and clamps a percentage-based expansion step.
func calculatePercentageStep(
	currentDec *inf.Dec,
	stepVal intstr.IntOrString,
	policy *apiv1.ExpansionPolicy,
) (*inf.Dec, error) {
	percent, err := parsePercentage(stepVal)
	if err != nil {
		return nil, fmt.Errorf("invalid percentage step: %w", err)
	}

	// expansionStep = currentSize * (percent / 100)
	expansionStepDec := new(inf.Dec).Mul(currentDec, inf.NewDec(int64(percent), 0))
	expansionStepDec = new(inf.Dec).QuoRound(expansionStepDec, inf.NewDec(100, 0), 0, inf.RoundDown)

	// Parse min and max step constraints
	minStepQty := parseQuantityOrDefault(policy.MinStep, defaultMinStep)
	maxStepQty := parseQuantityOrDefault(policy.MaxStep, defaultMaxStep)

	minStepDec := minStepQty.AsDec()
	maxStepDec := maxStepQty.AsDec()

	// Clamp the step: max(minStep, min(rawStep, maxStep))
	if expansionStepDec.Cmp(minStepDec) < 0 {
		expansionStepDec = minStepDec
	} else if expansionStepDec.Cmp(maxStepDec) > 0 {
		expansionStepDec = maxStepDec
	}

	return expansionStepDec, nil
}

// parseQuantityOrDefault attempts to parse a quantity string, returning a default if empty or invalid.
func parseQuantityOrDefault(qtyStr string, defaultStr string) *resource.Quantity {
	if qtyStr == "" {
		qty, err := resource.ParseQuantity(defaultStr)
		if err != nil {
			// This should never happen with hardcoded defaults, but avoid panics in controller code.
			autoresizeLog.Error(err, "invalid hardcoded default quantity", "default", defaultStr)
			zero := resource.MustParse("0")
			return &zero
		}
		return &qty
	}

	qty, err := resource.ParseQuantity(qtyStr)
	if err != nil {
		autoresizeLog.Info("invalid quantity in auto-resize config, using default",
			"provided", qtyStr, "default", defaultStr, "error", err.Error())
		fallback, err := resource.ParseQuantity(defaultStr)
		if err != nil {
			// This should never happen with hardcoded defaults, but avoid panics in controller code.
			autoresizeLog.Error(err, "invalid hardcoded default quantity", "default", defaultStr)
			zero := resource.MustParse("0")
			return &zero
		}
		return &fallback
	}

	return &qty
}

// normalizeStep validates step values and rejects integer inputs.
// Integer values are ambiguous; users must provide a percentage string (e.g., "20%")
// or a quantity (e.g., "10Gi").
func normalizeStep(step intstr.IntOrString) (intstr.IntOrString, error) {
	if step.Type != intstr.Int {
		return step, nil
	}

	if step.IntVal == 0 {
		return step, nil
	}

	return step, fmt.Errorf(
		"integer step is not supported; use a percentage string (e.g., \"20%%\") or a quantity (e.g., \"10Gi\")")
}
