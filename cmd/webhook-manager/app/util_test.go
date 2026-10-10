/*
Copyright 2026 The Volcano Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package app

import (
	"context"
	"sort"
	"testing"

	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func validatingConfig(name string) *admissionregistrationv1.ValidatingWebhookConfiguration {
	return &admissionregistrationv1.ValidatingWebhookConfiguration{ObjectMeta: metav1.ObjectMeta{Name: name}}
}

func mutatingConfig(name string) *admissionregistrationv1.MutatingWebhookConfiguration {
	return &admissionregistrationv1.MutatingWebhookConfiguration{ObjectMeta: metav1.ObjectMeta{Name: name}}
}

func remainingValidating(t *testing.T, client *fake.Clientset) []string {
	t.Helper()
	list, err := client.AdmissionregistrationV1().ValidatingWebhookConfigurations().List(context.Background(), metav1.ListOptions{})
	if err != nil {
		t.Fatalf("listing validating configs: %v", err)
	}
	names := make([]string, 0, len(list.Items))
	for i := range list.Items {
		names = append(names, list.Items[i].Name)
	}
	sort.Strings(names)
	return names
}

func remainingMutating(t *testing.T, client *fake.Clientset) []string {
	t.Helper()
	list, err := client.AdmissionregistrationV1().MutatingWebhookConfigurations().List(context.Background(), metav1.ListOptions{})
	if err != nil {
		t.Fatalf("listing mutating configs: %v", err)
	}
	names := make([]string, 0, len(list.Items))
	for i := range list.Items {
		names = append(names, list.Items[i].Name)
	}
	sort.Strings(names)
	return names
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// TestSyncAdmissionWebhooksDeletesDisabled checks that only prefixed
// configurations absent from --enabled-admission are pruned, while enabled
// ones and configurations owned by other systems survive.
func TestSyncAdmissionWebhooksDeletesDisabled(t *testing.T) {
	client := fake.NewSimpleClientset(
		validatingConfig("volcano-admission-service-jobs-validate"), // enabled, keep
		validatingConfig("volcano-admission-service-pods-validate"), // disabled, delete
		validatingConfig("volcano-admission-service"),               // no trailing hyphen, not ours, keep
		validatingConfig("other-controller-validate"),               // different prefix, keep
		mutatingConfig("volcano-admission-service-pods-mutate"),     // enabled, keep
		mutatingConfig("volcano-admission-service-jobs-mutate"),     // disabled, delete
	)

	err := SyncAdmissionWebhooks(client, "/jobs/validate,/pods/mutate")
	if err != nil {
		t.Fatalf("SyncAdmissionWebhooks returned error: %v", err)
	}

	wantValidating := []string{
		"other-controller-validate",
		"volcano-admission-service",
		"volcano-admission-service-jobs-validate",
	}
	if got := remainingValidating(t, client); !equalStrings(got, wantValidating) {
		t.Errorf("validating configs after sync = %v, want %v", got, wantValidating)
	}

	wantMutating := []string{"volcano-admission-service-pods-mutate"}
	if got := remainingMutating(t, client); !equalStrings(got, wantMutating) {
		t.Errorf("mutating configs after sync = %v, want %v", got, wantMutating)
	}
}

// TestSyncAdmissionWebhooksRefusesEmptyEnabledSet checks that a blank or
// malformed --enabled-admission is rejected rather than deleting every
// volcano admission webhook configuration.
func TestSyncAdmissionWebhooksRefusesEmptyEnabledSet(t *testing.T) {
	for _, enabled := range []string{"", "   ", ","} {
		client := fake.NewSimpleClientset(
			validatingConfig("volcano-admission-service-jobs-validate"),
			mutatingConfig("volcano-admission-service-jobs-mutate"),
		)

		err := SyncAdmissionWebhooks(client, enabled)
		if err == nil {
			t.Errorf("enabledAdmission %q: expected error, got nil", enabled)
		}
		// Nothing must have been deleted.
		if got := remainingValidating(t, client); len(got) != 1 {
			t.Errorf("enabledAdmission %q: validating configs = %v, want the original 1 kept", enabled, got)
		}
		if got := remainingMutating(t, client); len(got) != 1 {
			t.Errorf("enabledAdmission %q: mutating configs = %v, want the original 1 kept", enabled, got)
		}
	}
}
