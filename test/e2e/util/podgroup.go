/*
Copyright 2021 The Volcano Authors.

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

package util

import (
	"context"
	"fmt"
	"strings"
	"time"

	. "github.com/onsi/gomega"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"

	schedulingv1beta1 "volcano.sh/apis/pkg/apis/scheduling/v1beta1"
)

// CreatePodGroup creates a PodGroup with the specified name in the namespace
func CreatePodGroup(ctx *TestContext, pg string, namespace string) *schedulingv1beta1.PodGroup {
	podGroup, err := ctx.Vcclient.SchedulingV1beta1().PodGroups(namespace).Create(context.TODO(), &schedulingv1beta1.PodGroup{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
			Name:      pg,
		},
		Spec: schedulingv1beta1.PodGroupSpec{
			MinResources: &v1.ResourceList{},
		},
	}, metav1.CreateOptions{})
	Expect(err).NotTo(HaveOccurred(), "failed to create pod group %s", pg)
	return podGroup
}

// DeletePodGroup deletes a PodGroup with the specified name in the namespace
func DeletePodGroup(ctx *TestContext, pg string, namespace string) {
	_, err := ctx.Vcclient.SchedulingV1beta1().PodGroups(namespace).Get(context.TODO(), pg, metav1.GetOptions{})
	Expect(err).NotTo(HaveOccurred(), "failed to get pod group %s", pg)
	err = ctx.Vcclient.SchedulingV1beta1().PodGroups(namespace).Delete(context.TODO(), pg, metav1.DeleteOptions{})
	Expect(err).NotTo(HaveOccurred(), "failed to delete pod group %s", pg)
}

// WaitPodGroupPhase waits for the PodGroup to be the specified state
func WaitPodGroupPhase(ctx *TestContext, podGroup *schedulingv1beta1.PodGroup, state schedulingv1beta1.PodGroupPhase) error {
	var additionalError error
	err := wait.Poll(100*time.Millisecond, FiveMinute, func() (bool, error) {
		podGroup, err := ctx.Vcclient.SchedulingV1beta1().PodGroups(podGroup.Namespace).Get(context.TODO(), podGroup.Name, metav1.GetOptions{})
		Expect(err).NotTo(HaveOccurred(), "failed to get pod group %s in namespace %s", podGroup.Name, podGroup.Namespace)
		expected := podGroup.Status.Phase == state
		if !expected {
			additionalError = fmt.Errorf("expected podGroup '%s' phase in %s, actual got %s", podGroup.Name,
				state, podGroup.Status.Phase)
		}
		return expected, nil
	})
	if err != nil && strings.Contains(err.Error(), TimeOutMessage) {
		return fmt.Errorf("[Wait time out]: %s", additionalError)
	}
	return err
}

// PodGroupIsReady returns whether the status of PodGroup is ready
func PodGroupIsReady(ctx *TestContext, namespace string) (bool, error) {
	pgs, err := ctx.Vcclient.SchedulingV1beta1().PodGroups(namespace).List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		return false, err
	}
	if pgs != nil && len(pgs.Items) == 0 {
		return false, fmt.Errorf("pod group not found")
	}

	for _, pg := range pgs.Items {
		if pg.Status.Phase != schedulingv1beta1.PodGroupPending {
			return true, nil
		}
	}

	return false, fmt.Errorf("pod group phase is Pending")
}
