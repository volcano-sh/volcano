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

package mpi

import (
	"bytes"
	"context"
	"flag"
	"fmt"
	"strconv"
	"strings"

	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	v1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/klog"
	"volcano.sh/volcano/pkg/apis/batch/v1alpha1"
	"volcano.sh/volcano/pkg/apis/helpers"
	jobhelpers "volcano.sh/volcano/pkg/controllers/job/helpers"
	pluginsinterface "volcano.sh/volcano/pkg/controllers/job/plugins/interface"
)

const (
	kubectlMountPath      = "/opt/kube"
	kubexecScriptName     = "kubexec.sh"
	hostfileName          = "hostfile"
	rshAgentEnvName       = "OMPI_MCA_plm_rsh_agent"
	hostfileEnvName       = "OMPI_MCA_orte_default_hostfile"
	configMapName         = "%s-mpi-config"
	configMountPath       = "/etc/mpi"
	defaultSlotsPerWorker = 1
	serviceAccountName    = "sa-mpi-exec"
	roleBindingName       = "sa-mpi-exec-role-binding"
	roleName              = "mpi-exec-role"
)

type mpiPlugin struct {
	// Arguments given for the plugin
	pluginArguments []string

	// Slots per worker
	slotsPerWorker map[int]int

	Clientset pluginsinterface.PluginClientset
}

// New creates mpi plugin.
func New(client pluginsinterface.PluginClientset, arguments []string) pluginsinterface.PluginInterface {
	plugin := mpiPlugin{pluginArguments: arguments, slotsPerWorker: make(map[int]int), Clientset: client}

	// Parse flags
	plugin.addFlags()

	return &plugin
}

func (m *mpiPlugin) Name() string {
	return "mpi"
}

func (m *mpiPlugin) OnPodCreate(pod *v1.Pod, job *v1alpha1.Job) error {
	// Create env variable for OMPI_MCA_plm_rsh_agent and OMPI_MCA_orte_default_hostfile
	if err := m.createEnvVars(pod, job); err != nil {
		klog.Errorf("create env var failed: %v", err)
		return fmt.Errorf("create env for job <%s/%s> with mpi plugin failed: %v", job.Namespace, job.Name, err)
	}

	// Mount config map
	if err := m.mountConfigMap(pod, job); err != nil {
		klog.Errorf("mount config map failed: %v", err)
		return fmt.Errorf("mount config map for job <%s/%s> with mpi plugin failed: %v", job.Namespace, job.Name, err)
	}

	// Set service account
	if len(pod.Spec.ServiceAccountName) > 0 {
		klog.Warningf("pod %s for job <%s/%s> with mpi plugin has set service account %s already",
			pod.Name, job.Namespace, job.Name, pod.Spec.ServiceAccountName)
	} else {
		pod.Spec.ServiceAccountName = serviceAccountName
	}

	return nil
}

func (m *mpiPlugin) OnJobAdd(job *v1alpha1.Job) error {
	if job.Status.ControlledResources["plugin-"+m.Name()] == m.Name() {
		return nil
	}

	// Ensure service account exists
	if err := m.createServiceAccountIfNotExists(job); err != nil {
		klog.Errorf("create service account failed: %v", err)
		return fmt.Errorf("create service account for job <%s/%s> with mpi plugin failed: %v", job.Namespace, job.Name, err)
	}

	// Create configmap for kubexec.sh and hostfile
	if err := m.createConfigMap(job); err != nil {
		klog.Errorf("create configmap failed: %v", err)
		return fmt.Errorf("create configmap for job <%s/%s> with mpi plugin failed: %v", job.Namespace, job.Name, err)
	}

	job.Status.ControlledResources["plugin-"+m.Name()] = m.Name()

	return nil
}

func (m *mpiPlugin) OnJobDelete(job *v1alpha1.Job) error {
	if job.Status.ControlledResources["plugin-"+m.Name()] == m.Name() {
		return nil
	}

	// Delete configmap
	cmName := makeConfigMapName(job.Name)
	if err := helpers.DeleteConfigmap(job, m.Clientset.KubeClients, cmName); err != nil {
		klog.Errorf("delete configmap failed: %v", err)
		return fmt.Errorf("delete configmap for job <%s/%s> with mpi plugin failed: %v", job.Namespace, job.Name, err)
	}

	delete(job.Status.ControlledResources, "plugin-"+m.Name())

	return nil
}

func (m *mpiPlugin) OnJobUpdate(job *v1alpha1.Job) error {
	return nil
}

func (m *mpiPlugin) addFlags() {
	flagSet := flag.NewFlagSet(m.Name(), flag.ContinueOnError)
	var slotsPerWorker string
	flagSet.StringVar(&slotsPerWorker, "slots-per-worker", "", "The slots per worker to "+
		"add to hostfile. The format is <task index>:<slots per worker>, separated by comma."+
		"Task index is starting from 0. Example: 0:1,1:2,2:3")

	if err := flagSet.Parse(m.pluginArguments); err != nil {
		klog.Errorf("plugin %s flagset parse failed, err: %v", m.Name(), err)
	}

	elems := strings.Split(slotsPerWorker, ",")
	for _, elem := range elems {
		parts := strings.Split(elem, ":")
		if len(parts) == 2 {
			taskIndex, _ := strconv.Atoi(parts[0])
			slots, _ := strconv.Atoi(parts[1])
			m.slotsPerWorker[taskIndex] = slots
		}
	}
}

func (m *mpiPlugin) createConfigMap(job *v1alpha1.Job) error {
	data := make(map[string]string)
	data[kubexecScriptName] = generateKubexecScript()
	data[hostfileName] = m.createHostfile(job)

	cmName := makeConfigMapName(job.Name)
	if err := helpers.CreateOrUpdateConfigMap(job, m.Clientset.KubeClients, data, cmName); err != nil {
		klog.Errorf("create or update configmap for job <%s/%s> with mpi plugin failed: %v",
			job.Namespace, job.Name, err)
		return err
	}
	return nil
}

func (m *mpiPlugin) mountConfigMap(pod *v1.Pod, job *v1alpha1.Job) error {
	cmName := makeConfigMapName(job.Name)

	scriptMode := int32(0555)
	volume := v1.Volume{Name: "mpi-config"}
	volume.ConfigMap = &v1.ConfigMapVolumeSource{
		LocalObjectReference: v1.LocalObjectReference{Name: cmName},
		Items: []v1.KeyToPath{
			{
				Key:  kubexecScriptName,
				Path: kubexecScriptName,
				Mode: &scriptMode,
			},
			{
				Key:  hostfileName,
				Path: hostfileName,
			},
		},
	}
	pod.Spec.Volumes = append(pod.Spec.Volumes, volume)

	for i := range pod.Spec.Containers {
		vm := v1.VolumeMount{
			Name:      "mpi-config",
			ReadOnly:  true,
			MountPath: configMountPath,
		}
		pod.Spec.Containers[i].VolumeMounts = append(pod.Spec.Containers[i].VolumeMounts, vm)
	}

	return nil
}

func (m *mpiPlugin) createEnvVars(pod *v1.Pod, job *v1alpha1.Job) error {
	for i := range pod.Spec.Containers {
		pod.Spec.Containers[i].Env = append(pod.Spec.Containers[i].Env, v1.EnvVar{
			Name:  rshAgentEnvName,
			Value: fmt.Sprintf("%s/%s", configMountPath, kubexecScriptName),
		})
		pod.Spec.Containers[i].Env = append(pod.Spec.Containers[i].Env, v1.EnvVar{
			Name:  hostfileEnvName,
			Value: fmt.Sprintf("%s/%s", configMountPath, hostfileName),
		})
	}
	return nil
}

func (m *mpiPlugin) createHostfile(job *v1alpha1.Job) string {
	var buffer bytes.Buffer

	for taskIndex := range job.Spec.Tasks {
		slotsPerWorker := defaultSlotsPerWorker
		if len(m.slotsPerWorker) > 0 {
			if slots, ok := m.slotsPerWorker[taskIndex]; ok {
				slotsPerWorker = slots
			} else {
				continue
			}
		}
		replicas := job.Spec.Tasks[taskIndex].Replicas
		for i := int32(0); i < replicas; i++ {
			buffer.WriteString(fmt.Sprintf("%s slots=%d\n",
				jobhelpers.MakePodName(job.Name, job.Spec.Tasks[taskIndex].Name, int(i)),
				slotsPerWorker))
		}
	}

	return buffer.String()
}

func (m *mpiPlugin) createServiceAccountIfNotExists(job *v1alpha1.Job) error {
	if _, err := m.Clientset.KubeClients.CoreV1().ServiceAccounts(job.Namespace).Get(context.TODO(),
		serviceAccountName, metav1.GetOptions{}); err != nil {
		if !errors.IsNotFound(err) {
			return err
		}
	} else {
		return nil
	}

	// Create service account
	sa := v1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{Name: serviceAccountName},
	}
	if _, err := m.Clientset.KubeClients.CoreV1().ServiceAccounts(job.Namespace).Create(context.TODO(),
		&sa, metav1.CreateOptions{}); err != nil {
		if errors.IsAlreadyExists(err) {
			// Service account is created, assume that role binding is created as well
			klog.Warningf("service account %s already exists", serviceAccountName)
			return nil
		}
		return err
	}
	klog.Infof("service account %s is created in namespace %s", serviceAccountName, job.Namespace)

	// Create role
	role := rbacv1.Role{
		ObjectMeta: metav1.ObjectMeta{Name: roleName},
		Rules: []rbacv1.PolicyRule{
			{
				Verbs:           []string{"create"},
				APIGroups:       []string{""},
				Resources:       []string{"pods/exec"},
				ResourceNames:   nil,
				NonResourceURLs: nil,
			},
			{
				Verbs:     []string{"get", "list", "watch"},
				APIGroups: []string{""},
				Resources: []string{"pods"},
			},
		},
	}
	if _, err := m.Clientset.KubeClients.RbacV1().Roles(job.Namespace).Create(context.TODO(),
		&role, metav1.CreateOptions{}); err != nil {
		if !errors.IsAlreadyExists(err) {
			return err
		}
	}
	klog.Infof("role %s is created in namespace %s", roleName, job.Namespace)

	// Create role binding
	roleBinding := rbacv1.RoleBinding{
		ObjectMeta: metav1.ObjectMeta{Name: roleBindingName},
		Subjects: []rbacv1.Subject{
			{
				Kind:      "ServiceAccount",
				APIGroup:  "",
				Name:      serviceAccountName,
				Namespace: job.Namespace,
			},
		},
		RoleRef: rbacv1.RoleRef{
			Kind:     "Role",
			Name:     roleName,
			APIGroup: "rbac.authorization.k8s.io",
		},
	}
	if _, err := m.Clientset.KubeClients.RbacV1().RoleBindings(job.Namespace).Create(context.TODO(),
		&roleBinding, metav1.CreateOptions{}); err != nil {
		if !errors.IsAlreadyExists(err) {
			return err
		}
	}
	klog.Infof("rolebinding %s is created in namespace %s", roleBindingName, job.Namespace)
	return nil
}

func makeConfigMapName(jobName string) string {
	return fmt.Sprintf(configMapName, jobName)
}

func generateKubexecScript() string {
	kubexec := fmt.Sprintf(`#!/bin/sh
set -x
POD_NAME=$1
shift
%s/kubectl exec ${POD_NAME} -- /bin/sh -c "$*"
`, kubectlMountPath)
	return kubexec
}
