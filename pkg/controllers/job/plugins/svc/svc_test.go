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
	"errors"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	v1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"

	batch "volcano.sh/apis/pkg/apis/batch/v1alpha1"
	"volcano.sh/apis/pkg/apis/helpers"
	pluginsinterface "volcano.sh/volcano/pkg/controllers/job/plugins/interface"
)

const (
	testNamespace = "test-ns"
	testJobName   = "test-job"
	// testCMName is the hosts ConfigMap name the plugin derives from the job name.
	testCMName = testJobName + "-svc"
	// controlledKey marks a job whose resources were set up by the plugin.
	controlledKey = "plugin-svc"
)

var errAPIServer = errors.New("api server unavailable")

// failOn makes the fake client return errAPIServer for the given verb and resource.
func failOn(client *fake.Clientset, verb, resource string) {
	client.PrependReactor(verb, resource, func(k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, errAPIServer
	})
}

func newTestJob(tasks ...batch.TaskSpec) *batch.Job {
	return &batch.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      testJobName,
			Namespace: testNamespace,
			UID:       types.UID("test-job-uid"),
		},
		Spec: batch.JobSpec{Tasks: tasks},
		Status: batch.JobStatus{
			ControlledResources: map[string]string{},
		},
	}
}

func newTask(name string, replicas int32) batch.TaskSpec {
	return batch.TaskSpec{Name: name, Replicas: replicas}
}

func newTestPlugin(t *testing.T, args []string, objects ...runtime.Object) (*servicePlugin, *fake.Clientset) {
	t.Helper()
	client := fake.NewSimpleClientset(objects...)
	plugin, ok := New(pluginsinterface.PluginClientset{KubeClients: client}, args).(*servicePlugin)
	require.True(t, ok, "New should return a *servicePlugin")
	return plugin, client
}

func TestNew(t *testing.T) {
	tests := []struct {
		name                         string
		args                         []string
		wantPublishNotReadyAddresses bool
		wantDisableNetworkPolicy     bool
		wantInjectHostsEnv           bool
	}{
		{
			name:               "defaults",
			wantInjectHostsEnv: true,
		},
		{
			name:                         "publish not ready addresses",
			args:                         []string{"--publish-not-ready-addresses"},
			wantPublishNotReadyAddresses: true,
			wantInjectHostsEnv:           true,
		},
		{
			name:                     "disable network policy",
			args:                     []string{"--disable-network-policy=true"},
			wantDisableNetworkPolicy: true,
			wantInjectHostsEnv:       true,
		},
		{
			name:               "disable hosts env injection",
			args:               []string{"--inject-hosts-env=false"},
			wantInjectHostsEnv: false,
		},
		{
			name:                         "all flags set",
			args:                         []string{"--publish-not-ready-addresses", "--disable-network-policy", "--inject-hosts-env=false"},
			wantPublishNotReadyAddresses: true,
			wantDisableNetworkPolicy:     true,
			wantInjectHostsEnv:           false,
		},
		{
			name:               "unknown flag keeps defaults",
			args:               []string{"--unknown-flag"},
			wantInjectHostsEnv: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			plugin, _ := newTestPlugin(t, tt.args)

			assert.Equal(t, "svc", plugin.Name())
			assert.Equal(t, tt.wantPublishNotReadyAddresses, plugin.publishNotReadyAddresses)
			assert.Equal(t, tt.wantDisableNetworkPolicy, plugin.disableNetworkPolicy)
			assert.Equal(t, tt.wantInjectHostsEnv, plugin.injectHostsEnv)
		})
	}
}

func TestGenerateHosts(t *testing.T) {
	tests := []struct {
		name  string
		tasks []batch.TaskSpec
		want  map[string]string
	}{
		{
			name:  "one host per replica",
			tasks: []batch.TaskSpec{newTask("worker", 3)},
			want: map[string]string{
				"worker.host":     "test-job-worker-0.test-job\ntest-job-worker-1.test-job\ntest-job-worker-2.test-job",
				"VC_WORKER_HOSTS": "test-job-worker-0.test-job,test-job-worker-1.test-job,test-job-worker-2.test-job",
				"VC_WORKER_NUM":   "3",
			},
		},
		{
			name:  "dashes in task name",
			tasks: []batch.TaskSpec{newTask("param-server", 1)},
			want: map[string]string{
				"param_server.host":     "test-job-param-server-0.test-job",
				"VC_PARAM_SERVER_HOSTS": "test-job-param-server-0.test-job",
				"VC_PARAM_SERVER_NUM":   "1",
			},
		},
		{
			name: "custom hostname yields a single entry",
			tasks: []batch.TaskSpec{{
				Name:     "master",
				Replicas: 3,
				Template: v1.PodTemplateSpec{Spec: v1.PodSpec{Hostname: "master-host"}},
			}},
			want: map[string]string{
				"master.host":     "master-host.test-job",
				"VC_MASTER_HOSTS": "master-host.test-job",
				"VC_MASTER_NUM":   "1",
			},
		},
		{
			name: "custom subdomain",
			tasks: []batch.TaskSpec{{
				Name:     "worker",
				Replicas: 2,
				Template: v1.PodTemplateSpec{Spec: v1.PodSpec{Subdomain: "custom"}},
			}},
			want: map[string]string{
				"worker.host":     "test-job-worker-0.custom\ntest-job-worker-1.custom",
				"VC_WORKER_HOSTS": "test-job-worker-0.custom,test-job-worker-1.custom",
				"VC_WORKER_NUM":   "2",
			},
		},
		{
			name:  "zero replicas",
			tasks: []batch.TaskSpec{newTask("worker", 0)},
			want: map[string]string{
				"worker.host":     "",
				"VC_WORKER_HOSTS": "",
				"VC_WORKER_NUM":   "0",
			},
		},
		{
			name:  "multiple tasks",
			tasks: []batch.TaskSpec{newTask("ps", 1), newTask("worker", 2)},
			want: map[string]string{
				"ps.host":         "test-job-ps-0.test-job",
				"VC_PS_HOSTS":     "test-job-ps-0.test-job",
				"VC_PS_NUM":       "1",
				"worker.host":     "test-job-worker-0.test-job\ntest-job-worker-1.test-job",
				"VC_WORKER_HOSTS": "test-job-worker-0.test-job,test-job-worker-1.test-job",
				"VC_WORKER_NUM":   "2",
			},
		},
		{
			name: "no tasks",
			want: map[string]string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, GenerateHosts(newTestJob(tt.tasks...)))
		})
	}
}

func TestOnPodCreate(t *testing.T) {
	hostsEnv := func(name string) v1.EnvVar {
		return v1.EnvVar{
			Name: name,
			ValueFrom: &v1.EnvVarSource{
				ConfigMapKeyRef: &v1.ConfigMapKeySelector{
					LocalObjectReference: v1.LocalObjectReference{Name: testCMName},
					Key:                  name,
				},
			},
		}
	}
	existingEnv := v1.EnvVar{Name: "EXISTING", Value: "value"}
	wantVolume := v1.Volume{
		Name: testCMName,
		VolumeSource: v1.VolumeSource{
			ConfigMap: &v1.ConfigMapVolumeSource{
				LocalObjectReference: v1.LocalObjectReference{Name: testCMName},
			},
		},
	}
	wantMount := v1.VolumeMount{Name: testCMName, MountPath: ConfigMapMountPath}

	tests := []struct {
		name          string
		args          []string
		tasks         []batch.TaskSpec
		podSpec       v1.PodSpec
		wantHostname  string
		wantSubdomain string
		wantHostsEnv  []v1.EnvVar
	}{
		{
			name:          "defaults hostname and subdomain and injects hosts env for every task",
			tasks:         []batch.TaskSpec{newTask("param-server", 1), newTask("worker", 2)},
			podSpec:       v1.PodSpec{Containers: []v1.Container{{Name: "main"}}},
			wantHostname:  "test-job-worker-0",
			wantSubdomain: testJobName,
			wantHostsEnv: []v1.EnvVar{
				hostsEnv("VC_PARAM_SERVER_HOSTS"),
				hostsEnv("VC_PARAM_SERVER_NUM"),
				hostsEnv("VC_WORKER_HOSTS"),
				hostsEnv("VC_WORKER_NUM"),
			},
		},
		{
			name:  "keeps user set hostname and subdomain",
			tasks: []batch.TaskSpec{newTask("worker", 1)},
			podSpec: v1.PodSpec{
				Hostname:   "custom-host",
				Subdomain:  "custom-subdomain",
				Containers: []v1.Container{{Name: "main"}},
			},
			wantHostname:  "custom-host",
			wantSubdomain: "custom-subdomain",
			wantHostsEnv:  []v1.EnvVar{hostsEnv("VC_WORKER_HOSTS"), hostsEnv("VC_WORKER_NUM")},
		},
		{
			name:  "updates init containers and keeps existing env",
			tasks: []batch.TaskSpec{newTask("worker", 1)},
			podSpec: v1.PodSpec{
				InitContainers: []v1.Container{{Name: "init", Env: []v1.EnvVar{existingEnv}}},
				Containers:     []v1.Container{{Name: "main", Env: []v1.EnvVar{existingEnv}}, {Name: "sidecar"}},
			},
			wantHostname:  "test-job-worker-0",
			wantSubdomain: testJobName,
			wantHostsEnv:  []v1.EnvVar{hostsEnv("VC_WORKER_HOSTS"), hostsEnv("VC_WORKER_NUM")},
		},
		{
			name:  "inject hosts env disabled still mounts the hosts configmap",
			args:  []string{"--inject-hosts-env=false"},
			tasks: []batch.TaskSpec{newTask("worker", 1)},
			podSpec: v1.PodSpec{
				InitContainers: []v1.Container{{Name: "init"}},
				Containers:     []v1.Container{{Name: "main", Env: []v1.EnvVar{existingEnv}}},
			},
			wantHostname:  "test-job-worker-0",
			wantSubdomain: testJobName,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			plugin, _ := newTestPlugin(t, tt.args)
			job := newTestJob(tt.tasks...)
			pod := &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{Name: "test-job-worker-0", Namespace: testNamespace},
				Spec:       *tt.podSpec.DeepCopy(),
			}

			require.NoError(t, plugin.OnPodCreate(pod, job))

			assert.Equal(t, tt.wantHostname, pod.Spec.Hostname)
			assert.Equal(t, tt.wantSubdomain, pod.Spec.Subdomain)
			assert.Equal(t, []v1.Volume{wantVolume}, pod.Spec.Volumes)

			original := slices.Concat(tt.podSpec.InitContainers, tt.podSpec.Containers)
			updated := slices.Concat(pod.Spec.InitContainers, pod.Spec.Containers)
			require.Len(t, updated, len(original))
			for i, c := range updated {
				wantEnv := slices.Concat(original[i].Env, tt.wantHostsEnv)
				assert.Equal(t, wantEnv, c.Env, "env of container %q", c.Name)
				assert.Equal(t, []v1.VolumeMount{wantMount}, c.VolumeMounts, "volume mounts of container %q", c.Name)
			}
		})
	}
}

func TestOnJobAdd(t *testing.T) {
	existingService := &v1.Service{
		ObjectMeta: metav1.ObjectMeta{Name: testJobName, Namespace: testNamespace},
		Spec:       v1.ServiceSpec{ClusterIP: "10.0.0.1"},
	}
	existingNetworkPolicy := &networkingv1.NetworkPolicy{
		ObjectMeta: metav1.ObjectMeta{Name: testJobName, Namespace: testNamespace},
		Spec: networkingv1.NetworkPolicySpec{
			PolicyTypes: []networkingv1.PolicyType{networkingv1.PolicyTypeEgress},
		},
	}

	tests := []struct {
		name                    string
		args                    []string
		objects                 []runtime.Object
		alreadyControlled       bool
		callTwice               bool
		failVerb                string
		failResource            string
		wantCreated             bool
		wantNetworkPolicy       bool
		wantPublishNotReady     bool
		wantExistingObjectsKept bool
	}{
		{
			name:              "creates configmap, headless service and network policy",
			wantCreated:       true,
			wantNetworkPolicy: true,
		},
		{
			name:                "publish not ready addresses",
			args:                []string{"--publish-not-ready-addresses"},
			wantCreated:         true,
			wantNetworkPolicy:   true,
			wantPublishNotReady: true,
		},
		{
			name:        "network policy disabled",
			args:        []string{"--disable-network-policy"},
			wantCreated: true,
		},
		{
			name:                    "keeps existing service and network policy",
			objects:                 []runtime.Object{existingService, existingNetworkPolicy},
			wantCreated:             true,
			wantNetworkPolicy:       true,
			wantExistingObjectsKept: true,
		},
		{
			name:              "second call is a no-op",
			callTwice:         true,
			wantCreated:       true,
			wantNetworkPolicy: true,
		},
		{
			name:              "skips a job already handled by the plugin",
			alreadyControlled: true,
		},
		{
			name:         "returns error when configmap creation fails",
			failVerb:     "create",
			failResource: "configmaps",
		},
		{
			name:         "returns error when service lookup fails",
			failVerb:     "get",
			failResource: "services",
		},
		{
			name:         "returns error when service creation fails",
			failVerb:     "create",
			failResource: "services",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.TODO()
			plugin, client := newTestPlugin(t, tt.args, tt.objects...)
			job := newTestJob(newTask("worker", 2))
			if tt.alreadyControlled {
				job.Status.ControlledResources[controlledKey] = plugin.Name()
			}

			if tt.failVerb != "" {
				failOn(client, tt.failVerb, tt.failResource)
				err := plugin.OnJobAdd(job)
				assert.ErrorIs(t, err, errAPIServer)
				assert.NotContains(t, job.Status.ControlledResources, controlledKey)
				return
			}

			require.NoError(t, plugin.OnJobAdd(job))
			if tt.callTwice {
				require.NoError(t, plugin.OnJobAdd(job))
			}
			assert.Equal(t, plugin.Name(), job.Status.ControlledResources[controlledKey])

			cm, cmErr := client.CoreV1().ConfigMaps(testNamespace).Get(ctx, testCMName, metav1.GetOptions{})
			svc, svcErr := client.CoreV1().Services(testNamespace).Get(ctx, testJobName, metav1.GetOptions{})
			np, npErr := client.NetworkingV1().NetworkPolicies(testNamespace).Get(ctx, testJobName, metav1.GetOptions{})
			if !tt.wantCreated {
				assert.True(t, apierrors.IsNotFound(cmErr), "configmap should not be created")
				assert.True(t, apierrors.IsNotFound(svcErr), "service should not be created")
				assert.True(t, apierrors.IsNotFound(npErr), "network policy should not be created")
				return
			}

			wantOwners := []metav1.OwnerReference{*metav1.NewControllerRef(job, helpers.JobKind)}
			wantSelector := map[string]string{
				batch.JobNameKey:      testJobName,
				batch.JobNamespaceKey: testNamespace,
			}

			require.NoError(t, cmErr)
			assert.Equal(t, GenerateHosts(job), cm.Data)
			assert.Equal(t, wantOwners, cm.OwnerReferences)

			require.NoError(t, svcErr)
			if tt.wantExistingObjectsKept {
				assert.Equal(t, existingService.Spec, svc.Spec)
			} else {
				assert.Equal(t, v1.ClusterIPNone, svc.Spec.ClusterIP)
				assert.Equal(t, wantSelector, svc.Spec.Selector)
				assert.Equal(t, tt.wantPublishNotReady, svc.Spec.PublishNotReadyAddresses)
				assert.Equal(t, wantOwners, svc.OwnerReferences)
			}

			if !tt.wantNetworkPolicy {
				assert.True(t, apierrors.IsNotFound(npErr), "network policy should not be created")
				return
			}
			require.NoError(t, npErr)
			if tt.wantExistingObjectsKept {
				assert.Equal(t, existingNetworkPolicy.Spec, np.Spec)
				return
			}
			wantPolicySpec := networkingv1.NetworkPolicySpec{
				PodSelector: metav1.LabelSelector{MatchLabels: wantSelector},
				Ingress: []networkingv1.NetworkPolicyIngressRule{{
					From: []networkingv1.NetworkPolicyPeer{{
						PodSelector: &metav1.LabelSelector{MatchLabels: wantSelector},
					}},
				}},
				PolicyTypes: []networkingv1.PolicyType{networkingv1.PolicyTypeIngress},
			}
			assert.Equal(t, wantPolicySpec, np.Spec)
			assert.Equal(t, wantOwners, np.OwnerReferences)
		})
	}
}

func TestOnJobDelete(t *testing.T) {
	newPluginObjects := func() []runtime.Object {
		return []runtime.Object{
			&v1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: testCMName, Namespace: testNamespace}},
			&v1.Service{ObjectMeta: metav1.ObjectMeta{Name: testJobName, Namespace: testNamespace}},
			&networkingv1.NetworkPolicy{ObjectMeta: metav1.ObjectMeta{Name: testJobName, Namespace: testNamespace}},
		}
	}

	tests := []struct {
		name                  string
		args                  []string
		objects               []runtime.Object
		controlled            bool
		failVerb              string
		failResource          string
		wantDeleted           bool
		wantNetworkPolicyKept bool
	}{
		{
			name:        "deletes configmap, service and network policy",
			objects:     newPluginObjects(),
			controlled:  true,
			wantDeleted: true,
		},
		{
			name:                  "network policy disabled leaves network policy alone",
			args:                  []string{"--disable-network-policy"},
			objects:               newPluginObjects(),
			controlled:            true,
			wantDeleted:           true,
			wantNetworkPolicyKept: true,
		},
		{
			name:        "resources already deleted",
			controlled:  true,
			wantDeleted: true,
		},
		{
			name:    "skips a job not handled by the plugin",
			objects: newPluginObjects(),
		},
		{
			name:         "returns error when configmap deletion fails",
			objects:      newPluginObjects(),
			controlled:   true,
			failVerb:     "delete",
			failResource: "configmaps",
		},
		{
			name:         "returns error when service deletion fails",
			objects:      newPluginObjects(),
			controlled:   true,
			failVerb:     "delete",
			failResource: "services",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.TODO()
			plugin, client := newTestPlugin(t, tt.args, tt.objects...)
			job := newTestJob(newTask("worker", 1))
			if tt.controlled {
				job.Status.ControlledResources[controlledKey] = plugin.Name()
			}

			if tt.failVerb != "" {
				failOn(client, tt.failVerb, tt.failResource)
				err := plugin.OnJobDelete(job)
				assert.ErrorIs(t, err, errAPIServer)
				assert.Equal(t, plugin.Name(), job.Status.ControlledResources[controlledKey],
					"job should stay marked so the deletion is retried")
				return
			}

			require.NoError(t, plugin.OnJobDelete(job))
			assert.NotContains(t, job.Status.ControlledResources, controlledKey)

			_, cmErr := client.CoreV1().ConfigMaps(testNamespace).Get(ctx, testCMName, metav1.GetOptions{})
			_, svcErr := client.CoreV1().Services(testNamespace).Get(ctx, testJobName, metav1.GetOptions{})
			_, npErr := client.NetworkingV1().NetworkPolicies(testNamespace).Get(ctx, testJobName, metav1.GetOptions{})
			if !tt.wantDeleted {
				assert.NoError(t, cmErr, "configmap should be kept")
				assert.NoError(t, svcErr, "service should be kept")
				assert.NoError(t, npErr, "network policy should be kept")
				return
			}
			assert.True(t, apierrors.IsNotFound(cmErr), "configmap should be deleted")
			assert.True(t, apierrors.IsNotFound(svcErr), "service should be deleted")
			if tt.wantNetworkPolicyKept {
				assert.NoError(t, npErr, "network policy should be kept")
			} else {
				assert.True(t, apierrors.IsNotFound(npErr), "network policy should be deleted")
			}
		})
	}
}

func TestOnJobUpdate(t *testing.T) {
	staleConfigMap := &v1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: testCMName, Namespace: testNamespace},
		Data: map[string]string{
			"worker.host":     "test-job-worker-0.test-job",
			"VC_WORKER_HOSTS": "test-job-worker-0.test-job",
			"VC_WORKER_NUM":   "1",
		},
	}

	tests := []struct {
		name    string
		objects []runtime.Object
	}{
		{
			name: "creates missing configmap",
		},
		{
			name:    "refreshes hosts after the job is scaled",
			objects: []runtime.Object{staleConfigMap},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			plugin, client := newTestPlugin(t, nil, tt.objects...)
			job := newTestJob(newTask("worker", 3))

			require.NoError(t, plugin.OnJobUpdate(job))

			cm, err := client.CoreV1().ConfigMaps(testNamespace).Get(context.TODO(), testCMName, metav1.GetOptions{})
			require.NoError(t, err)
			assert.Equal(t, GenerateHosts(job), cm.Data)
		})
	}
}
