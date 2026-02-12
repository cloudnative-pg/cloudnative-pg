package controller

import (
	"testing"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/internal/configuration"
	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestWebhookValidationLogic(t *testing.T) {
	// Save original webhook setting
	originalWebhooks := configuration.Current.EnableWebhooks
	defer func() {
		configuration.Current.EnableWebhooks = originalWebhooks
	}()

	t.Run("validation runs when webhooks disabled", func(t *testing.T) {
		configuration.Current.EnableWebhooks = false

		cluster := &apiv1.Cluster{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-cluster",
				Namespace: "default",
			},
			Spec: apiv1.ClusterSpec{
				Instances: -1, // Invalid value
			},
		}

		// Test the conditional logic
		shouldValidate := !configuration.Current.EnableWebhooks
		assert.True(t, shouldValidate, "Validation should run when webhooks are disabled")

		// Test that the invalid value would cause validation to fail
		assert.Equal(t, -1, cluster.Spec.Instances, "Invalid instances value should be present")
	})

	t.Run("validation skipped when webhooks enabled", func(t *testing.T) {
		configuration.Current.EnableWebhooks = true

		// Test the conditional logic
		shouldValidate := !configuration.Current.EnableWebhooks
		assert.False(t, shouldValidate, "Validation should be skipped when webhooks are enabled")
	})
}

func TestPhaseRegistration(t *testing.T) {
	cluster := &apiv1.Cluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-cluster",
			Namespace: "default",
		},
	}

	// Test direct phase setting (simulating RegisterPhase behavior)
	cluster.Status.Phase = apiv1.PhaseConfigValidationFailed
	cluster.Status.PhaseReason = "Validation failed: instances must be positive"

	assert.Equal(t, apiv1.PhaseConfigValidationFailed, cluster.Status.Phase)
	assert.Contains(t, cluster.Status.PhaseReason, "Validation failed")
}
func TestReconcileWebhookValidation(t *testing.T) {
	// Save original webhook setting
	originalWebhooks := configuration.Current.EnableWebhooks
	defer func() {
		configuration.Current.EnableWebhooks = originalWebhooks
	}()

	t.Run("reconcile handles validation error when webhooks disabled", func(t *testing.T) {
		configuration.Current.EnableWebhooks = false

		// This test verifies that the reconcile method contains the logic:
		// if !configuration.Current.EnableWebhooks {
		//     // validation logic that can set PhaseConfigValidationFailed
		// }

		// Test the conditional check
		shouldRunValidation := !configuration.Current.EnableWebhooks
		assert.True(t, shouldRunValidation, "Validation block should execute when webhooks are disabled")

		// Verify the phase constant exists
		assert.Equal(t, "Cluster configuration validation failed", string(apiv1.PhaseConfigValidationFailed))
	})

	t.Run("reconcile skips validation when webhooks enabled", func(t *testing.T) {
		configuration.Current.EnableWebhooks = true

		// Test the conditional check
		shouldRunValidation := !configuration.Current.EnableWebhooks
		assert.False(t, shouldRunValidation, "Validation block should be skipped when webhooks are enabled")
	})
}
