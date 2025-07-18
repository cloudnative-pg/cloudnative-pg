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

package utils

import (
	"testing"
	"time"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestIsJobFailed(t *testing.T) {
	tests := []struct {
		name     string
		job      batchv1.Job
		expected bool
	}{
		{
			name: "job with failed condition",
			job: batchv1.Job{
				Status: batchv1.JobStatus{
					Conditions: []batchv1.JobCondition{
						{
							Type:   batchv1.JobFailed,
							Status: corev1.ConditionTrue,
						},
					},
				},
			},
			expected: true,
		},
		{
			name: "job without failed condition",
			job: batchv1.Job{
				Status: batchv1.JobStatus{
					Conditions: []batchv1.JobCondition{
						{
							Type:   batchv1.JobComplete,
							Status: corev1.ConditionTrue,
						},
					},
				},
			},
			expected: false,
		},
		{
			name: "job with no conditions",
			job: batchv1.Job{
				Status: batchv1.JobStatus{},
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := IsJobFailed(tt.job)
			if result != tt.expected {
				t.Errorf("IsJobFailed() = %v, expected %v", result, tt.expected)
			}
		})
	}
}

func TestIsJobComplete(t *testing.T) {
	tests := []struct {
		name     string
		job      batchv1.Job
		expected bool
	}{
		{
			name: "job with complete condition",
			job: batchv1.Job{
				Status: batchv1.JobStatus{
					Conditions: []batchv1.JobCondition{
						{
							Type:   batchv1.JobComplete,
							Status: corev1.ConditionTrue,
						},
					},
				},
			},
			expected: true,
		},
		{
			name: "job without complete condition",
			job: batchv1.Job{
				Status: batchv1.JobStatus{
					Conditions: []batchv1.JobCondition{
						{
							Type:   batchv1.JobFailed,
							Status: corev1.ConditionTrue,
						},
					},
				},
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := IsJobComplete(tt.job)
			if result != tt.expected {
				t.Errorf("IsJobComplete() = %v, expected %v", result, tt.expected)
			}
		})
	}
}

func TestIsJobStuck(t *testing.T) {
	now := time.Now()
	timeout := 10 * time.Minute

	tests := []struct {
		name     string
		job      batchv1.Job
		timeout  time.Duration
		expected bool
	}{
		{
			name: "stuck job - old with no active pods",
			job: batchv1.Job{
				ObjectMeta: metav1.ObjectMeta{
					CreationTimestamp: metav1.Time{Time: now.Add(-15 * time.Minute)},
				},
				Status: batchv1.JobStatus{
					Active:    0,
					Succeeded: 0,
					Failed:    0,
				},
			},
			timeout:  timeout,
			expected: true,
		},
		{
			name: "not stuck - recent job",
			job: batchv1.Job{
				ObjectMeta: metav1.ObjectMeta{
					CreationTimestamp: metav1.Time{Time: now.Add(-5 * time.Minute)},
				},
				Status: batchv1.JobStatus{
					Active:    0,
					Succeeded: 0,
					Failed:    0,
				},
			},
			timeout:  timeout,
			expected: false,
		},
		{
			name: "not stuck - has active pods",
			job: batchv1.Job{
				ObjectMeta: metav1.ObjectMeta{
					CreationTimestamp: metav1.Time{Time: now.Add(-15 * time.Minute)},
				},
				Status: batchv1.JobStatus{
					Active:    1,
					Succeeded: 0,
					Failed:    0,
				},
			},
			timeout:  timeout,
			expected: false,
		},
		{
			name: "not stuck - already completed",
			job: batchv1.Job{
				ObjectMeta: metav1.ObjectMeta{
					CreationTimestamp: metav1.Time{Time: now.Add(-15 * time.Minute)},
				},
				Status: batchv1.JobStatus{
					Active:    0,
					Succeeded: 1,
					Failed:    0,
					Conditions: []batchv1.JobCondition{
						{
							Type:   batchv1.JobComplete,
							Status: corev1.ConditionTrue,
						},
					},
				},
			},
			timeout:  timeout,
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := IsJobStuck(tt.job, tt.timeout)
			if result != tt.expected {
				t.Errorf("IsJobStuck() = %v, expected %v", result, tt.expected)
			}
		})
	}
}

func TestIsJobFailedOrStuck(t *testing.T) {
	now := time.Now()
	timeout := 10 * time.Minute

	tests := []struct {
		name     string
		job      batchv1.Job
		expected bool
	}{
		{
			name: "failed job",
			job: batchv1.Job{
				Status: batchv1.JobStatus{
					Conditions: []batchv1.JobCondition{
						{
							Type:   batchv1.JobFailed,
							Status: corev1.ConditionTrue,
						},
					},
				},
			},
			expected: true,
		},
		{
			name: "stuck job",
			job: batchv1.Job{
				ObjectMeta: metav1.ObjectMeta{
					CreationTimestamp: metav1.Time{Time: now.Add(-15 * time.Minute)},
				},
				Status: batchv1.JobStatus{
					Active:    0,
					Succeeded: 0,
					Failed:    0,
				},
			},
			expected: true,
		},
		{
			name: "healthy job",
			job: batchv1.Job{
				ObjectMeta: metav1.ObjectMeta{
					CreationTimestamp: metav1.Time{Time: now.Add(-5 * time.Minute)},
				},
				Status: batchv1.JobStatus{
					Active:    1,
					Succeeded: 0,
					Failed:    0,
				},
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := IsJobFailedOrStuck(tt.job, timeout)
			if result != tt.expected {
				t.Errorf("IsJobFailedOrStuck() = %v, expected %v", result, tt.expected)
			}
		})
	}
}
