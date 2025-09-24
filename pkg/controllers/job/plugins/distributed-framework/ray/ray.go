/*
Copyright 2025 The Volcano Authors.

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

package ray

import (
	"context"
	"flag"
	"fmt"

	v1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/klog/v2"

	batch "volcano.sh/apis/pkg/apis/batch/v1alpha1"
	"volcano.sh/apis/pkg/apis/helpers"
	jobhelpers "volcano.sh/volcano/pkg/controllers/job/helpers"
	pluginsinterface "volcano.sh/volcano/pkg/controllers/job/plugins/interface"
)

const (
	// RayPluginName is the name of the plugin
	RayPluginName = "ray"
	// DefaultHead is the default task name of head node.
	DefaultHead = "head"
	// DefaultHeadContainer is the default container name of head node.
	DefaultHeadContainer = "head"
	// DefaultWorker is the default task name of worker node.
	DefaultWorker = "worker"
	// DefaultWorkerContainer is the default container name of worker node.
	DefaultWorkerContainer = "worker"
	// DefaultPort is the default port to expose a Global Control Storage management.
	DefaultPort = 6379
	// DefaultDashboardPort is the default port to expose a ray dashboard.
	DefaultDashboardPort = 8265
	// DefaultClientPort is the default port to connect to a client.
	DefaultClientPort = 10001
	// GcsPortName is the port name for a Global Control Storage management.
	GcsPortName = "gcs"
	// DashboardPortName is the port name for a ray dashboard.
	DashboardPortName = "dashboard"
	// ClientServerPortName is the port name for a ray client api
	ClientServerPortName = "client-server"
)

type rayPlugin struct {
	rayArguments        []string
	clientset           pluginsinterface.PluginClientset
	headName            string
	headContainerName   string
	workerName          string
	workerContainerName string
	port                int
	dashboardPort       int
	clientPort          int
}

// New creates ray plugin.
func New(client pluginsinterface.PluginClientset, arguments []string) pluginsinterface.PluginInterface {
	rp := rayPlugin{rayArguments: arguments, clientset: client}
	rp.addFlags()
	return &rp
}

func (rp *rayPlugin) addFlags() {
	flagSet := flag.NewFlagSet(rp.Name(), flag.ContinueOnError)
	flagSet.StringVar(&rp.headName, "head", DefaultHead, "name of head node in ray cluster")
	flagSet.StringVar(&rp.headContainerName, "headContainer", DefaultHeadContainer, "The container name in a head task pod")
	flagSet.StringVar(&rp.workerName, "worker", DefaultWorker, "name of worker node in ray cluster")
	flagSet.StringVar(&rp.workerContainerName, "workerContainer", DefaultWorkerContainer, "The container name in a worker task pod")
	flagSet.IntVar(&rp.port, "port", DefaultPort, "The port for GCS")
	flagSet.IntVar(&rp.dashboardPort, "dashboardPort", DefaultDashboardPort, "The port for the Ray dashboard")
	flagSet.IntVar(&rp.clientPort, "clientPort", DefaultClientPort, "The port for the Ray client server")
	if err := flagSet.Parse(rp.rayArguments); err != nil {
		klog.Errorf("plugin %s flagset parse failed, err: %v", rp.Name(), err)
	}
}

func (rp *rayPlugin) Name() string {
	return RayPluginName
}

func (rp *rayPlugin) OnPodCreate(pod *v1.Pod, job *batch.Job) error {
	taskSpec := jobhelpers.GetTaskKey(pod)

	headIndex := jobhelpers.GetTaskIndexUnderJob(rp.headName, job)
	// Check whether a head task exists
	if headIndex == -1 {
		return fmt.Errorf("job %v doesn't have head task %v", job.Name, rp.headName)
	}

	for i, c := range pod.Spec.Containers {
		if taskSpec == rp.headName && c.Name == rp.headContainerName {
			rp.openHeadContainerPort(&pod.Spec.Containers[i], i, pod)

			var headCommand []string
			headCommand = append(headCommand, "sh")
			headCommand = append(headCommand, "-c")
			headCommand = append(headCommand, fmt.Sprintf("ray start --head --block --dashboard-host=0.0.0.0 --port=%v --dashboard-port=%v --ray-client-server-port=%v", rp.port, rp.dashboardPort, rp.clientPort))
			pod.Spec.Containers[i].Command = headCommand
		}

		if taskSpec == rp.workerName && c.Name == rp.workerContainerName {
			headAddr := rp.generateHeadAddr(job.Spec.Tasks[headIndex], job.Name)
			headEndpoint := fmt.Sprintf("%v:%v", headAddr, rp.port)
			var workerCommand []string
			workerCommand = append(workerCommand, "sh")
			workerCommand = append(workerCommand, "-c")
			workerCommand = append(workerCommand, fmt.Sprintf("ray start --block --address=%v", headEndpoint))
			pod.Spec.Containers[i].Command = workerCommand
		}
	}

	return nil
}

func (rp *rayPlugin) generateHeadAddr(task batch.TaskSpec, jobName string) string {
	hostName := task.Template.Spec.Hostname
	subdomain := task.Template.Spec.Subdomain
	// If hostname does not exist, pod name[metadata.name] replace it.
	if hostName == "" {
		hostName = jobhelpers.MakePodName(jobName, task.Name, 0)
	}
	if subdomain == "" {
		subdomain = jobName
	}

	host := hostName + "." + subdomain
	return host
}

func (rp *rayPlugin) openHeadContainerPort(c *v1.Container, index int, pod *v1.Pod) {
	hasPort := false
	hasDashboardPort := false
	hasClientPort := false
	for _, p := range c.Ports {
		if p.ContainerPort == int32(rp.port) {
			hasPort = true
		}
		if p.ContainerPort == int32(rp.dashboardPort) {
			hasDashboardPort = true
		}
		if p.ContainerPort == int32(rp.clientPort) {
			hasClientPort = true
		}

		if hasPort && hasDashboardPort && hasClientPort {
			break
		}
	}

	// This code adds a ray GCS port
	if !hasPort {
		port := v1.ContainerPort{
			Name:          GcsPortName,
			ContainerPort: int32(rp.port),
		}
		pod.Spec.Containers[index].Ports = append(pod.Spec.Containers[index].Ports, port)
	}

	// This code adds a ray dashboard Port
	if !hasDashboardPort {
		dashboardPort := v1.ContainerPort{
			Name:          DashboardPortName,
			ContainerPort: int32(rp.dashboardPort),
		}
		pod.Spec.Containers[index].Ports = append(pod.Spec.Containers[index].Ports, dashboardPort)
	}

	// This code adds a ray client server port
	if !hasClientPort {
		clientPort := v1.ContainerPort{
			Name:          ClientServerPortName,
			ContainerPort: int32(rp.clientPort),
		}
		pod.Spec.Containers[index].Ports = append(pod.Spec.Containers[index].Ports, clientPort)
	}
}

func (rp *rayPlugin) OnJobAdd(job *batch.Job) error {
	if job.Status.ControlledResources["plugin-"+rp.Name()] == rp.Name() {
		return nil
	}

	// When the Volcano Job is created, also create a Service for the head node
	if err := rp.createServiceIfNotExist(job); err != nil {
		return err
	}

	job.Status.ControlledResources["plugin-"+rp.Name()] = rp.Name()
	return nil
}

func (rp *rayPlugin) OnJobDelete(job *batch.Job) error {
	if job.Status.ControlledResources["plugin-"+rp.Name()] != rp.Name() {
		return nil
	}

	// When OnJobDelete is called, the head node Service is deleted
	headServiceName := job.Name + "-head-svc"
	if err := rp.clientset.KubeClients.CoreV1().Services(job.Namespace).Delete(context.TODO(), headServiceName, metav1.DeleteOptions{}); err != nil {
		if !apierrors.IsNotFound(err) {
			klog.Errorf("Failed to delete Service of Job %v/%v: %v", job.Namespace, headServiceName, err)
			return err
		}
	}
	delete(job.Status.ControlledResources, "plugin-"+rp.Name())
	return nil
}

func (rp *rayPlugin) OnJobUpdate(job *batch.Job) error {
	return nil
}

func (rp *rayPlugin) createServiceIfNotExist(job *batch.Job) error {
	// If Service does not exist, create one for Job.
	headServiceName := job.Name + "-head-svc"
	if _, err := rp.clientset.KubeClients.CoreV1().Services(job.Namespace).Get(context.TODO(), headServiceName, metav1.GetOptions{}); err != nil {
		if !apierrors.IsNotFound(err) {
			klog.V(3).Infof("Failed to get Service for Job <%s/%s>: %v",
				job.Namespace, job.Name, err)
			return err
		}

		svc := &v1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: job.Namespace,
				Name:      headServiceName,
				OwnerReferences: []metav1.OwnerReference{
					*metav1.NewControllerRef(job, helpers.JobKind),
				},
			},
			Spec: v1.ServiceSpec{
				Selector: map[string]string{
					batch.JobNameKey:      job.Name,
					batch.JobNamespaceKey: job.Namespace,
					batch.TaskSpecKey:     rp.headName,
				},
				Ports: []v1.ServicePort{
					{
						Name:       GcsPortName,
						Port:       int32(rp.port),
						TargetPort: intstr.FromString(GcsPortName),
						Protocol:   v1.ProtocolTCP,
					},
					{
						Name:       DashboardPortName,
						Port:       int32(rp.dashboardPort),
						TargetPort: intstr.FromString(DashboardPortName),
						Protocol:   v1.ProtocolTCP,
					},
					{
						Name:       ClientServerPortName,
						Port:       int32(rp.clientPort),
						TargetPort: intstr.FromString(ClientServerPortName),
						Protocol:   v1.ProtocolTCP,
					},
				},
			},
		}

		if _, e := rp.clientset.KubeClients.CoreV1().Services(job.Namespace).Create(context.TODO(), svc, metav1.CreateOptions{}); e != nil {
			klog.V(3).Infof("Failed to create Service for Job <%s/%s>: %v", job.Namespace, headServiceName, e)
			return e
		}
	}

	return nil
}
