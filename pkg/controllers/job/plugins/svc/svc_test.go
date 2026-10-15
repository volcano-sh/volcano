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

package svc

import (
	"context"
	"testing"

	v1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/fake"

	batch "volcano.sh/apis/pkg/apis/batch/v1alpha1"
	pluginsinterface "volcano.sh/volcano/pkg/controllers/job/plugins/interface"
)

func TestOnJobDeleteKeepsResourcesNotOwnedByJob(t *testing.T) {
	const namespace = "team-a"
	const jobName = "default-deny"

	// Objects created by the namespace owner, not by the Job.
	kubeClient := fake.NewSimpleClientset(
		&networkingv1.NetworkPolicy{ObjectMeta: metav1.ObjectMeta{Namespace: namespace, Name: jobName}},
		&v1.Service{ObjectMeta: metav1.ObjectMeta{Namespace: namespace, Name: jobName}},
		&v1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{Namespace: namespace, Name: jobName + "-svc"},
			Data:       map[string]string{"app.conf": "keep"},
		},
	)

	job := &batch.Job{
		ObjectMeta: metav1.ObjectMeta{Namespace: namespace, Name: jobName, UID: types.UID("job-uid")},
		Spec: batch.JobSpec{
			Tasks: []batch.TaskSpec{{Name: "worker", Replicas: 1}},
		},
		Status: batch.JobStatus{ControlledResources: map[string]string{}},
	}

	plugin := New(pluginsinterface.PluginClientset{KubeClients: kubeClient}, nil)

	if err := plugin.OnJobAdd(job); err == nil {
		t.Errorf("expected OnJobAdd to fail on a ConfigMap not controlled by the Job")
	}
	cm, err := kubeClient.CoreV1().ConfigMaps(namespace).Get(context.TODO(), jobName+"-svc", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("ConfigMap not owned by the Job was removed: %v", err)
	}
	if cm.Data["app.conf"] != "keep" {
		t.Errorf("ConfigMap not owned by the Job was overwritten: %v", cm.Data)
	}

	job.Status.ControlledResources["plugin-"+plugin.Name()] = plugin.Name()
	if err := plugin.OnJobDelete(job); err != nil {
		t.Fatalf("OnJobDelete failed: %v", err)
	}

	if _, err := kubeClient.NetworkingV1().NetworkPolicies(namespace).Get(context.TODO(), jobName, metav1.GetOptions{}); err != nil {
		t.Errorf("NetworkPolicy not owned by the Job was deleted: %v", err)
	}
	if _, err := kubeClient.CoreV1().Services(namespace).Get(context.TODO(), jobName, metav1.GetOptions{}); err != nil {
		t.Errorf("Service not owned by the Job was deleted: %v", err)
	}
	if _, err := kubeClient.CoreV1().ConfigMaps(namespace).Get(context.TODO(), jobName+"-svc", metav1.GetOptions{}); err != nil {
		t.Errorf("ConfigMap not owned by the Job was deleted: %v", err)
	}
}

func TestOnJobDeleteRemovesResourcesOwnedByJob(t *testing.T) {
	job := &batch.Job{
		ObjectMeta: metav1.ObjectMeta{Namespace: "team-a", Name: "train", UID: types.UID("job-uid")},
		Spec: batch.JobSpec{
			Tasks: []batch.TaskSpec{{Name: "worker", Replicas: 1}},
		},
		Status: batch.JobStatus{ControlledResources: map[string]string{}},
	}
	kubeClient := fake.NewSimpleClientset()
	plugin := New(pluginsinterface.PluginClientset{KubeClients: kubeClient}, nil)

	if err := plugin.OnJobAdd(job); err != nil {
		t.Fatalf("OnJobAdd failed: %v", err)
	}
	if err := plugin.OnJobUpdate(job); err != nil {
		t.Fatalf("OnJobUpdate failed: %v", err)
	}
	if err := plugin.OnJobDelete(job); err != nil {
		t.Fatalf("OnJobDelete failed: %v", err)
	}

	if _, err := kubeClient.NetworkingV1().NetworkPolicies("team-a").Get(context.TODO(), "train", metav1.GetOptions{}); err == nil {
		t.Errorf("NetworkPolicy owned by the Job was not deleted")
	}
	if _, err := kubeClient.CoreV1().Services("team-a").Get(context.TODO(), "train", metav1.GetOptions{}); err == nil {
		t.Errorf("Service owned by the Job was not deleted")
	}
	if _, err := kubeClient.CoreV1().ConfigMaps("team-a").Get(context.TODO(), "train-svc", metav1.GetOptions{}); err == nil {
		t.Errorf("ConfigMap owned by the Job was not deleted")
	}
}
