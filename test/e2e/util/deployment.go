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
	batchv1 "k8s.io/api/batch/v1"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/wait"
)

// CreateDeployment creates a new deployment
func CreateDeployment(ctx *TestContext, name string, rep int32, img string, req v1.ResourceList) *appv1.Deployment {
	deploymentName := "deployment.k8s.io"
	d := &appv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: ctx.Namespace,
		},
		Spec: appv1.DeploymentSpec{
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
					SchedulerName: "volcano",
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

	deployment, err := ctx.Kubeclient.AppsV1().Deployments(ctx.Namespace).Create(context.TODO(), d, metav1.CreateOptions{})
	Expect(err).NotTo(HaveOccurred(), "failed to create deployment %s", name)

	return deployment
}

func deploymentReady(ctx *TestContext, name string) wait.ConditionFunc {
	return func() (bool, error) {
		deployment, err := ctx.Kubeclient.AppsV1().Deployments(ctx.Namespace).Get(context.TODO(), name, metav1.GetOptions{})
		Expect(err).NotTo(HaveOccurred(), "failed to get deployment %s in namespace %s", name, ctx.Namespace)

		pods, err := ctx.Kubeclient.CoreV1().Pods(ctx.Namespace).List(context.TODO(), metav1.ListOptions{})
		Expect(err).NotTo(HaveOccurred(), "failed to list pods in namespace %s", ctx.Namespace)

		labelSelector := labels.SelectorFromSet(deployment.Spec.Selector.MatchLabels)

		readyTaskNum := 0
		for _, pod := range pods.Items {
			if !labelSelector.Matches(labels.Set(pod.Labels)) {
				continue
			}
			if pod.DeletionTimestamp != nil {
				return false, nil
			}
			if pod.Status.Phase == v1.PodRunning || pod.Status.Phase == v1.PodSucceeded {
				readyTaskNum++
			}
		}

		return *(deployment.Spec.Replicas) == int32(readyTaskNum), nil
	}
}

func WaitDeploymentReady(ctx *TestContext, name string) error {
	return wait.Poll(100*time.Millisecond, FiveMinute, deploymentReady(ctx, name))
}

func DeleteDeployment(ctx *TestContext, name string) error {
	foreground := metav1.DeletePropagationForeground
	return ctx.Kubeclient.AppsV1().Deployments(ctx.Namespace).Delete(context.TODO(), name, metav1.DeleteOptions{
		PropagationPolicy: &foreground,
	})
}

// CreateSampleK8sJob creates a new k8s job
func CreateSampleK8sJob(ctx *TestContext, name string, img string, req v1.ResourceList) *batchv1.Job {
	k8sjobname := "job.k8s.io"
	defaultTrue := true
	j := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Spec: batchv1.JobSpec{
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					k8sjobname: name,
				},
			},
			ManualSelector: &defaultTrue,
			Template: v1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{k8sjobname: name},
				},
				Spec: v1.PodSpec{
					SchedulerName: "volcano",
					RestartPolicy: v1.RestartPolicyOnFailure,
					Containers: []v1.Container{
						{
							Image:           img,
							Name:            name,
							Command:         []string{"/bin/sh", "-c", "sleep 10"},
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

	jb, err := ctx.Kubeclient.BatchV1().Jobs(ctx.Namespace).Create(context.TODO(), j, metav1.CreateOptions{})
	Expect(err).NotTo(HaveOccurred(), "failed to create k8sjob %s", name)

	return jb
}

func k8sjobCompleted(ctx *TestContext, name string) wait.ConditionFunc {
	return func() (bool, error) {
		jb, err := ctx.Kubeclient.BatchV1().Jobs(ctx.Namespace).Get(context.TODO(), name, metav1.GetOptions{})
		Expect(err).NotTo(HaveOccurred(), "failed to get k8sjob %s in namespace %s", name, ctx.Namespace)

		pods, err := ctx.Kubeclient.CoreV1().Pods(ctx.Namespace).List(context.TODO(), metav1.ListOptions{})
		Expect(err).NotTo(HaveOccurred(), "failed to list pods in namespace %s", ctx.Namespace)

		labelSelector := labels.SelectorFromSet(jb.Spec.Selector.MatchLabels)

		for _, pod := range pods.Items {
			if !labelSelector.Matches(labels.Set(pod.Labels)) {
				continue
			}
			if pod.Status.Phase == v1.PodSucceeded {
				return true, nil
			}
		}

		return false, nil
	}
}

func Waitk8sJobCompleted(ctx *TestContext, name string) error {
	return wait.Poll(100*time.Millisecond, FiveMinute, k8sjobCompleted(ctx, name))
}

func DeleteK8sJob(ctx *TestContext, name string) error {
	foreground := metav1.DeletePropagationForeground
	return ctx.Kubeclient.BatchV1().Jobs(ctx.Namespace).Delete(context.TODO(), name, metav1.DeleteOptions{
		PropagationPolicy: &foreground,
	})
}
