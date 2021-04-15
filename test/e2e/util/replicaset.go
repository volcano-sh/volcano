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
	"time"

	. "github.com/onsi/gomega"
	appv1 "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/wait"
)

// CreateReplicaSet creates a new replica set
func CreateReplicaSet(ctx *TestContext, name string, rep int32, img string, req v1.ResourceList) *appv1.ReplicaSet {
	deploymentName := "deployment.k8s.io"
	deployment := &appv1.ReplicaSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: ctx.Namespace,
		},
		Spec: appv1.ReplicaSetSpec{
			Replicas: &rep,
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					deploymentName: name,
				},
			},
			Template: v1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{deploymentName: name},
				},
				Spec: v1.PodSpec{
					RestartPolicy: v1.RestartPolicyAlways,
					Containers: []v1.Container{
						{
							Image:           img,
							Name:            name,
							ImagePullPolicy: v1.PullIfNotPresent,
							Resources: v1.ResourceRequirements{
								Requests: req,
							},
						},
					},
				},
			},
		},
	}

	deployment, err := ctx.Kubeclient.AppsV1().ReplicaSets(ctx.Namespace).Create(context.TODO(), deployment, metav1.CreateOptions{})
	Expect(err).NotTo(HaveOccurred(), "failed to create replica sets %s", name)

	return deployment
}

func replicaSetReady(ctx *TestContext, name string) wait.ConditionFunc {
	return func() (bool, error) {
		deployment, err := ctx.Kubeclient.AppsV1().ReplicaSets(ctx.Namespace).Get(context.TODO(), name, metav1.GetOptions{})
		Expect(err).NotTo(HaveOccurred(), "failed to get replica set %s in namespace %s", name, ctx.Namespace)

		pods, err := ctx.Kubeclient.CoreV1().Pods(ctx.Namespace).List(context.TODO(), metav1.ListOptions{})
		Expect(err).NotTo(HaveOccurred(), "failed to list pods in namespace %s", ctx.Namespace)

		labelSelector := labels.SelectorFromSet(deployment.Spec.Selector.MatchLabels)

		readyTaskNum := 0
		for _, pod := range pods.Items {
			if !labelSelector.Matches(labels.Set(pod.Labels)) {
				continue
			}
			if pod.Status.Phase == v1.PodRunning || pod.Status.Phase == v1.PodSucceeded {
				readyTaskNum++
			}
		}

		return *(deployment.Spec.Replicas) == int32(readyTaskNum), nil
	}
}

func WaitReplicaSetReady(ctx *TestContext, name string) error {
	return wait.Poll(100*time.Millisecond, FiveMinute, replicaSetReady(ctx, name))
}

func DeleteReplicaSet(ctx *TestContext, name string) error {
	foreground := metav1.DeletePropagationForeground
	return ctx.Kubeclient.AppsV1().ReplicaSets(ctx.Namespace).Delete(context.TODO(), name, metav1.DeleteOptions{
		PropagationPolicy: &foreground,
	})
}
