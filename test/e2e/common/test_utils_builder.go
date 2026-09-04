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

package common

import (
	"fmt"
	"strings"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	batchv1alpha1 "volcano.sh/apis/pkg/apis/batch/v1alpha1"
)

const (
	DefaultNginxImage = "nginx:1.29.3-alpine"
)

// PodBuilder is a fluent API for building test pods.
type PodBuilder struct {
	name              string
	namespace         string
	image             string
	labels            map[string]string
	annotations       map[string]string
	resourceRequests  v1.ResourceList
	resourceLimits    v1.ResourceList
	nodeName          string
	schedulerName     string
	restartPolicy     v1.RestartPolicy
	nodeSelector      map[string]string
	schedulingGates   []v1.PodSchedulingGate
	command           []string
	terminationGrace  *int64
}

// NewPodBuilder creates a new PodBuilder with the given name and namespace.
func NewPodBuilder(name, namespace string) *PodBuilder {
	return &PodBuilder{
		name:      name,
		namespace: namespace,
		image:     DefaultNginxImage,
	}
}

// WithImage sets the container image for the pod.
func (b *PodBuilder) WithImage(image string) *PodBuilder {
	b.image = image
	return b
}

// WithLabels sets the labels for the pod.
func (b *PodBuilder) WithLabels(labels map[string]string) *PodBuilder {
	b.labels = labels
	return b
}

// WithAnnotations sets the annotations for the pod.
func (b *PodBuilder) WithAnnotations(annotations map[string]string) *PodBuilder {
	b.annotations = annotations
	return b
}

// WithResourceRequests sets the resource requests for the pod container.
func (b *PodBuilder) WithResourceRequests(requests v1.ResourceList) *PodBuilder {
	b.resourceRequests = requests
	return b
}

// WithResourceLimits sets the resource limits for the pod container.
func (b *PodBuilder) WithResourceLimits(limits v1.ResourceList) *PodBuilder {
	b.resourceLimits = limits
	return b
}

// WithNodeName sets the node name for the pod.
func (b *PodBuilder) WithNodeName(nodeName string) *PodBuilder {
	b.nodeName = nodeName
	return b
}

// WithSchedulerName sets the scheduler name for the pod.
func (b *PodBuilder) WithSchedulerName(schedulerName string) *PodBuilder {
	b.schedulerName = schedulerName
	return b
}

// WithRestartPolicy sets the restart policy for the pod.
func (b *PodBuilder) WithRestartPolicy(policy v1.RestartPolicy) *PodBuilder {
	b.restartPolicy = policy
	return b
}

// WithNodeSelector sets the node selector for the pod.
func (b *PodBuilder) WithNodeSelector(selector map[string]string) *PodBuilder {
	b.nodeSelector = selector
	return b
}

// WithSchedulingGates sets the scheduling gates for the pod.
func (b *PodBuilder) WithSchedulingGates(gates []v1.PodSchedulingGate) *PodBuilder {
	b.schedulingGates = gates
	return b
}

// WithCommand sets the command for the pod container.
func (b *PodBuilder) WithCommand(command []string) *PodBuilder {
	b.command = command
	return b
}

// WithTerminationGracePeriodSeconds sets the termination grace period for the pod.
func (b *PodBuilder) WithTerminationGracePeriodSeconds(seconds *int64) *PodBuilder {
	b.terminationGrace = seconds
	return b
}

// Build returns a *v1.Pod constructed from the builder configuration.
func (b *PodBuilder) Build() *v1.Pod {
	meta := metav1.ObjectMeta{
		Name:      b.name,
		Namespace: b.namespace,
	}

	if len(b.annotations) > 0 {
		meta.Annotations = b.annotations
	}

	if len(b.labels) > 0 {
		meta.Labels = b.labels
	}

	container := v1.Container{
		Image:           b.image,
		Name:            b.name,
		ImagePullPolicy: v1.PullIfNotPresent,
		Resources: v1.ResourceRequirements{
			Requests: b.resourceRequests,
			Limits:   b.resourceLimits,
		},
	}

	if len(b.command) > 0 {
		container.Command = b.command
	}

	pod := &v1.Pod{
		ObjectMeta: meta,
		Spec: v1.PodSpec{
			NodeName:      b.nodeName,
			SchedulerName: b.schedulerName,
			Containers:    []v1.Container{container},
		},
	}

	if b.restartPolicy != "" {
		pod.Spec.RestartPolicy = b.restartPolicy
	}

	if len(b.nodeSelector) > 0 {
		pod.Spec.NodeSelector = b.nodeSelector
	}

	if len(b.schedulingGates) > 0 {
		pod.Spec.SchedulingGates = b.schedulingGates
	}

	if b.terminationGrace != nil {
		pod.Spec.TerminationGracePeriodSeconds = b.terminationGrace
	}

	return pod
}

// JobBuilder is a fluent API for building Volcano jobs.
type JobBuilder struct {
	name              string
	namespace         string
	queue             string
	minAvailable      int32
	schedulerName     string
	policies          []batchv1alpha1.LifecyclePolicy
	plugins           map[string][]string
	volumes           []batchv1alpha1.VolumeSpec
	ttl               *int32
	minSuccess        *int32
	maxRetry          int32
	networkTopology   *batchv1alpha1.NetworkTopologySpec
	tasks             []TaskBuilder
}

// TaskBuilder is a fluent API for building job tasks.
type TaskBuilder struct {
	name              string
	replicas          int32
	minAvailable      *int32
	image             string
	command           string
	workingDir        string
	hostport          int32
	resourceRequests  v1.ResourceList
	resourceLimits    v1.ResourceList
	affinity          *v1.Affinity
	labels            map[string]string
	annotations       map[string]string
	policies          []batchv1alpha1.LifecyclePolicy
	restartPolicy     v1.RestartPolicy
	tolerations       []v1.Toleration
	defaultGraceful   *int64
	taskPriority      string
	maxRetry          int32
	schedulingGates   []v1.PodSchedulingGate
	partitionPolicy   *batchv1alpha1.PartitionPolicySpec
	resourceClaims    []v1.PodResourceClaim
}

// NewJobBuilder creates a new JobBuilder with the given name and namespace.
func NewJobBuilder(name, namespace string) *JobBuilder {
	return &JobBuilder{
		name:          name,
		namespace:     namespace,
		schedulerName: "volcano",
	}
}

// WithQueue sets the queue for the job.
func (b *JobBuilder) WithQueue(queue string) *JobBuilder {
	b.queue = queue
	return b
}

// WithMinAvailable sets the minimum available replicas for the job.
func (b *JobBuilder) WithMinAvailable(min int32) *JobBuilder {
	b.minAvailable = min
	return b
}

// WithSchedulerName sets the scheduler name for the job.
func (b *JobBuilder) WithSchedulerName(schedulerName string) *JobBuilder {
	b.schedulerName = schedulerName
	return b
}

// WithPolicies sets the lifecycle policies for the job.
func (b *JobBuilder) WithPolicies(policies []batchv1alpha1.LifecyclePolicy) *JobBuilder {
	b.policies = policies
	return b
}

// WithPlugins sets the plugins for the job.
func (b *JobBuilder) WithPlugins(plugins map[string][]string) *JobBuilder {
	b.plugins = plugins
	return b
}

// WithVolumes sets the volumes for the job.
func (b *JobBuilder) WithVolumes(volumes []batchv1alpha1.VolumeSpec) *JobBuilder {
	b.volumes = volumes
	return b
}

// WithTTL sets the TTL seconds after finished for the job.
func (b *JobBuilder) WithTTL(ttl *int32) *JobBuilder {
	b.ttl = ttl
	return b
}

// WithMinSuccess sets the minimum success count for the job.
func (b *JobBuilder) WithMinSuccess(minSuccess *int32) *JobBuilder {
	b.minSuccess = minSuccess
	return b
}

// WithMaxRetry sets the maximum retry count for the job.
func (b *JobBuilder) WithMaxRetry(maxRetry int32) *JobBuilder {
	b.maxRetry = maxRetry
	return b
}

// WithNetworkTopology sets the network topology spec for the job.
func (b *JobBuilder) WithNetworkTopology(networkTopology *batchv1alpha1.NetworkTopologySpec) *JobBuilder {
	b.networkTopology = networkTopology
	return b
}

// AddTask adds a task to the job using a TaskBuilder.
func (b *JobBuilder) AddTask(taskBuilder *TaskBuilder) *JobBuilder {
	b.tasks = append(b.tasks, *taskBuilder)
	return b
}

// Build returns a *batchv1alpha1.Job constructed from the builder configuration.
func (b *JobBuilder) Build() *batchv1alpha1.Job {
	job := &batchv1alpha1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      b.name,
			Namespace: b.namespace,
		},
		Spec: batchv1alpha1.JobSpec{
			SchedulerName:           b.schedulerName,
			Policies:                b.policies,
			Queue:                   b.queue,
			Plugins:                 b.plugins,
			TTLSecondsAfterFinished: b.ttl,
			MinSuccess:              b.minSuccess,
			MaxRetry:                b.maxRetry,
			NetworkTopology:         b.networkTopology,
		},
	}

	var min int32
	for i, taskBuilder := range b.tasks {
		name := taskBuilder.name
		if len(name) == 0 {
			name = fmt.Sprintf("%s-task-%d", b.name, i)
		}

		restartPolicy := v1.RestartPolicyOnFailure
		if len(taskBuilder.restartPolicy) > 0 {
			restartPolicy = taskBuilder.restartPolicy
		}

		maxRetry := taskBuilder.maxRetry
		if maxRetry < 0 {
			maxRetry = 0
		}

		ts := batchv1alpha1.TaskSpec{
			Name:            name,
			Replicas:        taskBuilder.replicas,
			MinAvailable:    taskBuilder.minAvailable,
			Policies:        taskBuilder.policies,
			MaxRetry:        maxRetry,
			PartitionPolicy: taskBuilder.partitionPolicy,
			Template: v1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Name:        name,
					Labels:      taskBuilder.labels,
					Annotations: taskBuilder.annotations,
				},
				Spec: v1.PodSpec{
					RestartPolicy:     restartPolicy,
					Containers:        createContainers(taskBuilder.image, taskBuilder.command, taskBuilder.workingDir, taskBuilder.resourceRequests, taskBuilder.resourceLimits, taskBuilder.hostport),
					Affinity:          taskBuilder.affinity,
					Tolerations:       taskBuilder.tolerations,
					PriorityClassName: taskBuilder.taskPriority,
					SchedulingGates:   taskBuilder.schedulingGates,
					ResourceClaims:    taskBuilder.resourceClaims,
				},
			},
		}

		if taskBuilder.defaultGraceful != nil {
			ts.Template.Spec.TerminationGracePeriodSeconds = taskBuilder.defaultGraceful
		} else {
			var defaultPeriod int64 = 3
			ts.Template.Spec.TerminationGracePeriodSeconds = &defaultPeriod
		}

		job.Spec.Tasks = append(job.Spec.Tasks, ts)

		if taskBuilder.minAvailable != nil {
			min += *taskBuilder.minAvailable
		} else {
			min += taskBuilder.replicas
		}
	}

	if b.minAvailable > 0 {
		job.Spec.MinAvailable = b.minAvailable
	} else {
		job.Spec.MinAvailable = min
	}

	job.Spec.Volumes = b.volumes

	return job
}

// NewTaskBuilder creates a new TaskBuilder with the given name.
func NewTaskBuilder(name string) *TaskBuilder {
	return &TaskBuilder{
		name:  name,
		image: DefaultNginxImage,
	}
}

// WithReplicas sets the number of replicas for the task.
func (b *TaskBuilder) WithReplicas(replicas int32) *TaskBuilder {
	b.replicas = replicas
	return b
}

// WithMinAvailable sets the minimum available replicas for the task.
func (b *TaskBuilder) WithMinAvailable(min int32) *TaskBuilder {
	b.minAvailable = &min
	return b
}

// WithImage sets the container image for the task.
func (b *TaskBuilder) WithImage(image string) *TaskBuilder {
	b.image = image
	return b
}

// WithCommand sets the command for the task.
func (b *TaskBuilder) WithCommand(command string) *TaskBuilder {
	b.command = command
	return b
}

// WithWorkingDir sets the working directory for the task.
func (b *TaskBuilder) WithWorkingDir(workingDir string) *TaskBuilder {
	b.workingDir = workingDir
	return b
}

// WithHostPort sets the host port for the task.
func (b *TaskBuilder) WithHostPort(hostport int32) *TaskBuilder {
	b.hostport = hostport
	return b
}

// WithResourceRequests sets the resource requests for the task.
func (b *TaskBuilder) WithResourceRequests(requests v1.ResourceList) *TaskBuilder {
	b.resourceRequests = requests
	return b
}

// WithResourceLimits sets the resource limits for the task.
func (b *TaskBuilder) WithResourceLimits(limits v1.ResourceList) *TaskBuilder {
	b.resourceLimits = limits
	return b
}

// WithAffinity sets the affinity for the task.
func (b *TaskBuilder) WithAffinity(affinity *v1.Affinity) *TaskBuilder {
	b.affinity = affinity
	return b
}

// WithLabels sets the labels for the task.
func (b *TaskBuilder) WithLabels(labels map[string]string) *TaskBuilder {
	b.labels = labels
	return b
}

// WithAnnotations sets the annotations for the task.
func (b *TaskBuilder) WithAnnotations(annotations map[string]string) *TaskBuilder {
	b.annotations = annotations
	return b
}

// WithPolicies sets the lifecycle policies for the task.
func (b *TaskBuilder) WithPolicies(policies []batchv1alpha1.LifecyclePolicy) *TaskBuilder {
	b.policies = policies
	return b
}

// WithRestartPolicy sets the restart policy for the task.
func (b *TaskBuilder) WithRestartPolicy(policy v1.RestartPolicy) *TaskBuilder {
	b.restartPolicy = policy
	return b
}

// WithTolerations sets the tolerations for the task.
func (b *TaskBuilder) WithTolerations(tolerations []v1.Toleration) *TaskBuilder {
	b.tolerations = tolerations
	return b
}

// WithTerminationGracePeriodSeconds sets the termination grace period for the task.
func (b *TaskBuilder) WithTerminationGracePeriodSeconds(seconds *int64) *TaskBuilder {
	b.defaultGraceful = seconds
	return b
}

// WithTaskPriority sets the priority class for the task.
func (b *TaskBuilder) WithTaskPriority(priority string) *TaskBuilder {
	b.taskPriority = priority
	return b
}

// WithMaxRetry sets the maximum retry count for the task.
func (b *TaskBuilder) WithMaxRetry(maxRetry int32) *TaskBuilder {
	b.maxRetry = maxRetry
	return b
}

// WithSchedulingGates sets the scheduling gates for the task.
func (b *TaskBuilder) WithSchedulingGates(gates []v1.PodSchedulingGate) *TaskBuilder {
	b.schedulingGates = gates
	return b
}

// WithPartitionPolicy sets the partition policy for the task.
func (b *TaskBuilder) WithPartitionPolicy(policy *batchv1alpha1.PartitionPolicySpec) *TaskBuilder {
	b.partitionPolicy = policy
	return b
}

// WithResourceClaims sets the resource claims for the task.
func (b *TaskBuilder) WithResourceClaims(claims []v1.PodResourceClaim) *TaskBuilder {
	b.resourceClaims = claims
	return b
}

// createContainers is a helper function to create containers for a task.
func createContainers(img, command, workingDir string, req, limit v1.ResourceList, hostport int32) []v1.Container {
	var imageRepo []string
	container := v1.Container{
		Image:           img,
		ImagePullPolicy: v1.PullIfNotPresent,
		Resources: v1.ResourceRequirements{
			Requests: req,
			Limits:   limit,
		},
	}
	if !strings.Contains(img, ":") {
		imageRepo = strings.Split(img, "/")
	} else {
		imageRepo = strings.Split(img[:strings.Index(img, ":")], "/")
	}
	container.Name = imageRepo[len(imageRepo)-1]

	if len(command) > 0 {
		container.Command = []string{"/bin/sh"}
		container.Args = []string{"-c", command}
	}

	if hostport > 0 {
		container.Ports = []v1.ContainerPort{
			{
				ContainerPort: hostport,
				HostPort:      hostport,
			},
		}
	}

	if len(workingDir) > 0 {
		container.WorkingDir = workingDir
	}

	return []v1.Container{container}
}
