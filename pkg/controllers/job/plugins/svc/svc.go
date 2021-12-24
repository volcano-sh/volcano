/*
Copyright 2019 The Volcano Authors.

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
	"flag"
	"fmt"
	"strconv"
	"strings"

	v1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/klog"

	batch "volcano.sh/apis/pkg/apis/batch/v1alpha1"
	"volcano.sh/apis/pkg/apis/helpers"
	jobhelpers "volcano.sh/volcano/pkg/controllers/job/helpers"
	pluginsinterface "volcano.sh/volcano/pkg/controllers/job/plugins/interface"
)

type servicePlugin struct {
	// Arguments given for the plugin
	pluginArguments []string

	Clientset pluginsinterface.PluginClientset

	// flag parse args
	publishNotReadyAddresses bool
	disableNetworkPolicy     bool
}

// New creates service plugin.
func New(client pluginsinterface.PluginClientset, arguments []string) pluginsinterface.PluginInterface {
	servicePlugin := servicePlugin{pluginArguments: arguments, Clientset: client}

	servicePlugin.addFlags()

	return &servicePlugin
}

func (sp *servicePlugin) Name() string {
	return "svc"
}

func (sp *servicePlugin) addFlags() {
	flagSet := flag.NewFlagSet(sp.Name(), flag.ContinueOnError)
	flagSet.BoolVar(&sp.publishNotReadyAddresses, "publish-not-ready-addresses", sp.publishNotReadyAddresses,
		"set publishNotReadyAddresses of svc to true")
	flagSet.BoolVar(&sp.disableNetworkPolicy, "disable-network-policy", sp.disableNetworkPolicy,
		"set disableNetworkPolicy of svc to true")

	if err := flagSet.Parse(sp.pluginArguments); err != nil {
		klog.Errorf("plugin %s flagset parse failed, err: %v", sp.Name(), err)
	}
}

func (sp *servicePlugin) OnPodCreate(pod *v1.Pod, job *batch.Job) error {
	// Add `hostname` and `subdomain` for pod, mount service config for pod.
	// A pod with `hostname` and `subdomain` will have the fully qualified domain name(FQDN)
	// `hostname.subdomain.namespace.svc.cluster-domain.example`.
	// If there exists a headless service in the same namespace as the pod and with the
	// same name as the `subdomain`, the cluster's KubeDNS Server will returns an A record for
	// the Pods's fully qualified hostname, pointing to the Pod’s IP.
	// `hostname.subdomain` will be used as address of the pod.
	// By default, a client Pod’s DNS search list will include the Pod’s own namespace and
	// the cluster’s default domain, so the pod can be accessed by pods in the same namespace
	// through the address of pod.
	// More info: https://kubernetes.io/docs/concepts/services-networking/dns-pod-service
	if len(pod.Spec.Hostname) == 0 {
		pod.Spec.Hostname = pod.Name
	}
	if len(pod.Spec.Subdomain) == 0 {
		pod.Spec.Subdomain = job.Name
	}

	var hostEnv []v1.EnvVar
	var envNames []string

	for _, ts := range job.Spec.Tasks {
		// TODO(k82cn): The splitter and the prefix of env should be configurable.
		formateENVKey := strings.Replace(ts.Name, "-", "_", -1)
		envNames = append(envNames, fmt.Sprintf(EnvTaskHostFmt, strings.ToUpper(formateENVKey)))
		envNames = append(envNames, fmt.Sprintf(EnvHostNumFmt, strings.ToUpper(formateENVKey)))
	}

	for _, name := range envNames {
		hostEnv = append(hostEnv, v1.EnvVar{
			Name: name,
			ValueFrom: &v1.EnvVarSource{
				ConfigMapKeyRef: &v1.ConfigMapKeySelector{
					LocalObjectReference: v1.LocalObjectReference{Name: sp.cmName(job)},
					Key:                  name,
				}}},
		)
	}

	for i := range pod.Spec.Containers {
		pod.Spec.Containers[i].Env = append(pod.Spec.Containers[i].Env, hostEnv...)
	}

	for i := range pod.Spec.InitContainers {
		pod.Spec.InitContainers[i].Env = append(pod.Spec.InitContainers[i].Env, hostEnv...)
	}

	sp.mountConfigmap(pod, job)

	return nil
}

func (sp *servicePlugin) OnJobAdd(job *batch.Job) error {
	if job.Status.ControlledResources["plugin-"+sp.Name()] == sp.Name() {
		return nil
	}

	hostFile := GenerateHosts(job)

	// Create ConfigMap of hosts for Pods to mount.
	if err := helpers.CreateOrUpdateConfigMap(job, sp.Clientset.KubeClients, hostFile, sp.cmName(job)); err != nil {
		return err
	}

	if err := sp.createServiceIfNotExist(job); err != nil {
		return err
	}

	if !sp.disableNetworkPolicy {
		if err := sp.createNetworkPolicyIfNotExist(job); err != nil {
			return err
		}
	}
	job.Status.ControlledResources["plugin-"+sp.Name()] = sp.Name()

	return nil
}

func (sp *servicePlugin) OnJobDelete(job *batch.Job) error {
	if job.Status.ControlledResources["plugin-"+sp.Name()] != sp.Name() {
		return nil
	}

	if err := helpers.DeleteConfigmap(job, sp.Clientset.KubeClients, sp.cmName(job)); err != nil {
		return err
	}

	if err := sp.Clientset.KubeClients.CoreV1().Services(job.Namespace).Delete(context.TODO(), job.Name, metav1.DeleteOptions{}); err != nil {
		if !apierrors.IsNotFound(err) {
			klog.Errorf("Failed to delete Service of Job %v/%v: %v", job.Namespace, job.Name, err)
			return err
		}
	}
	delete(job.Status.ControlledResources, "plugin-"+sp.Name())

	if !sp.disableNetworkPolicy {
		if err := sp.Clientset.KubeClients.NetworkingV1().NetworkPolicies(job.Namespace).Delete(context.TODO(), job.Name, metav1.DeleteOptions{}); err != nil {
			if !apierrors.IsNotFound(err) {
				klog.Errorf("Failed to delete Network policy of Job %v/%v: %v", job.Namespace, job.Name, err)
				return err
			}
		}
	}
	return nil
}

func (sp *servicePlugin) OnJobUpdate(job *batch.Job) error {
	hostFile := GenerateHosts(job)

	// updates ConfigMap of hosts for Pods to mount.
	return helpers.CreateOrUpdateConfigMap(job, sp.Clientset.KubeClients, hostFile, sp.cmName(job))
}

func (sp *servicePlugin) mountConfigmap(pod *v1.Pod, job *batch.Job) {
	cmName := sp.cmName(job)
	cmVolume := v1.Volume{
		Name: cmName,
	}
	cmVolume.ConfigMap = &v1.ConfigMapVolumeSource{
		LocalObjectReference: v1.LocalObjectReference{
			Name: cmName,
		},
	}
	pod.Spec.Volumes = append(pod.Spec.Volumes, cmVolume)

	vm := v1.VolumeMount{
		MountPath: ConfigMapMountPath,
		Name:      cmName,
	}

	for i, c := range pod.Spec.Containers {
		pod.Spec.Containers[i].VolumeMounts = append(c.VolumeMounts, vm)
	}
	for i, c := range pod.Spec.InitContainers {
		pod.Spec.InitContainers[i].VolumeMounts = append(c.VolumeMounts, vm)
	}
}

func (sp *servicePlugin) createServiceIfNotExist(job *batch.Job) error {
	// If Service does not exist, create one for Job.
	if _, err := sp.Clientset.KubeClients.CoreV1().Services(job.Namespace).Get(context.TODO(), job.Name, metav1.GetOptions{}); err != nil {
		if !apierrors.IsNotFound(err) {
			klog.V(3).Infof("Failed to get Service for Job <%s/%s>: %v",
				job.Namespace, job.Name, err)
			return err
		}

		svc := &v1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: job.Namespace,
				Name:      job.Name,
				OwnerReferences: []metav1.OwnerReference{
					*metav1.NewControllerRef(job, helpers.JobKind),
				},
			},
			Spec: v1.ServiceSpec{
				ClusterIP: "None",
				Selector: map[string]string{
					batch.JobNameKey:      job.Name,
					batch.JobNamespaceKey: job.Namespace,
				},
				PublishNotReadyAddresses: sp.publishNotReadyAddresses,
				Ports: []v1.ServicePort{
					{
						Name:       "placeholder-volcano",
						Port:       1,
						Protocol:   v1.ProtocolTCP,
						TargetPort: intstr.FromInt(1),
					},
				},
			},
		}

		if _, e := sp.Clientset.KubeClients.CoreV1().Services(job.Namespace).Create(context.TODO(), svc, metav1.CreateOptions{}); e != nil {
			klog.V(3).Infof("Failed to create Service for Job <%s/%s>: %v", job.Namespace, job.Name, e)
			return e
		}
		job.Status.ControlledResources["plugin-"+sp.Name()] = sp.Name()
	}

	return nil
}

// Limit pods can be accessible only by pods belong to the job.
func (sp *servicePlugin) createNetworkPolicyIfNotExist(job *batch.Job) error {
	// If network policy does not exist, create one for Job.
	if _, err := sp.Clientset.KubeClients.NetworkingV1().NetworkPolicies(job.Namespace).Get(context.TODO(), job.Name, metav1.GetOptions{}); err != nil {
		if !apierrors.IsNotFound(err) {
			klog.V(3).Infof("Failed to get NetworkPolicy for Job <%s/%s>: %v",
				job.Namespace, job.Name, err)
			return err
		}

		networkpolicy := &networkingv1.NetworkPolicy{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: job.Namespace,
				Name:      job.Name,
				OwnerReferences: []metav1.OwnerReference{
					*metav1.NewControllerRef(job, helpers.JobKind),
				},
			},
			Spec: networkingv1.NetworkPolicySpec{
				PodSelector: metav1.LabelSelector{
					MatchLabels: map[string]string{
						batch.JobNameKey:      job.Name,
						batch.JobNamespaceKey: job.Namespace,
					},
				},
				Ingress: []networkingv1.NetworkPolicyIngressRule{{
					From: []networkingv1.NetworkPolicyPeer{{
						PodSelector: &metav1.LabelSelector{
							MatchLabels: map[string]string{
								batch.JobNameKey:      job.Name,
								batch.JobNamespaceKey: job.Namespace,
							},
						},
					}},
				}},
				PolicyTypes: []networkingv1.PolicyType{networkingv1.PolicyTypeIngress},
			},
		}

		if _, e := sp.Clientset.KubeClients.NetworkingV1().NetworkPolicies(job.Namespace).Create(context.TODO(), networkpolicy, metav1.CreateOptions{}); e != nil {
			klog.V(3).Infof("Failed to create Service for Job <%s/%s>: %v", job.Namespace, job.Name, e)
			return e
		}
		job.Status.ControlledResources["plugin-"+sp.Name()] = sp.Name()
	}

	return nil
}

func (sp *servicePlugin) cmName(job *batch.Job) string {
	return fmt.Sprintf("%s-%s", job.Name, sp.Name())
}

// GenerateHosts generates hostnames per task.
func GenerateHosts(job *batch.Job) map[string]string {
	hostFile := make(map[string]string, len(job.Spec.Tasks))

	for _, ts := range job.Spec.Tasks {
		hosts := make([]string, 0, ts.Replicas)

		for i := 0; i < int(ts.Replicas); i++ {
			hostName := ts.Template.Spec.Hostname
			subdomain := ts.Template.Spec.Subdomain
			if len(hostName) == 0 {
				hostName = jobhelpers.MakePodName(job.Name, ts.Name, i)
			}
			if len(subdomain) == 0 {
				subdomain = job.Name
			}
			hosts = append(hosts, hostName+"."+subdomain)
			if len(ts.Template.Spec.Hostname) != 0 {
				break
			}
		}

		formateENVKey := strings.Replace(ts.Name, "-", "_", -1)
		key := fmt.Sprintf(ConfigMapTaskHostFmt, formateENVKey)
		hostFile[key] = strings.Join(hosts, "\n")

		// TODO(k82cn): The splitter and the prefix of env should be configurable.
		// export hosts as environment
		key = fmt.Sprintf(EnvTaskHostFmt, strings.ToUpper(formateENVKey))
		hostFile[key] = strings.Join(hosts, ",")
		// export host number as environment.
		key = fmt.Sprintf(EnvHostNumFmt, strings.ToUpper(formateENVKey))
		hostFile[key] = strconv.Itoa(len(hosts))
	}

	return hostFile
}
