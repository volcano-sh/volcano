/*
Copyright 2019 The Kubernetes Authors.

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

package ssh

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"flag"
	"fmt"

	"golang.org/x/crypto/ssh"

	"github.com/golang/glog"

	"k8s.io/api/core/v1"

	vkv1 "github.com/kubernetes-sigs/kube-batch/pkg/apis/batch/v1alpha1"
	"github.com/kubernetes-sigs/kube-batch/pkg/apis/helpers"
	"github.com/kubernetes-sigs/kube-batch/pkg/controllers/job/plugins/env"
	vkinterface "github.com/kubernetes-sigs/kube-batch/pkg/controllers/job/plugins/interface"
)

type sshPlugin struct {
	// Arguments given for the plugin
	pluginArguments []string

	Clientset vkinterface.PluginClientset

	// flag parse args
	noRoot bool
}

// New is used to create pluginInterface from pluginClientSet
func New(client vkinterface.PluginClientset, arguments []string) vkinterface.PluginInterface {
	sshPlugin := sshPlugin{pluginArguments: arguments, Clientset: client}

	sshPlugin.addFlags()

	return &sshPlugin
}

func (sp *sshPlugin) Name() string {
	return "ssh"
}

func (sp *sshPlugin) OnPodCreate(pod *v1.Pod, job *vkv1.Job) error {
	sp.mountRsaKey(pod, job)

	return nil
}

func (sp *sshPlugin) OnJobAdd(job *vkv1.Job) error {
	if job.Status.ControlledResources["plugin-"+sp.Name()] == sp.Name() {
		return nil
	}

	data, err := generateRsaKey()
	if err != nil {
		return err
	}

	if err := helpers.CreateConfigMapIfNotExist(job, sp.Clientset.KubeClients, data, sp.cmName(job)); err != nil {
		return err
	}

	job.Status.ControlledResources["plugin-"+sp.Name()] = sp.Name()

	return nil
}

func (sp *sshPlugin) OnJobDelete(job *vkv1.Job) error {
	if err := helpers.DeleteConfigmap(job, sp.Clientset.KubeClients, sp.cmName(job)); err != nil {
		return err
	}

	return nil
}

func (sp *sshPlugin) mountRsaKey(pod *v1.Pod, job *vkv1.Job) {
	sshPath := SSHAbsolutePath
	if sp.noRoot {
		sshPath = env.ConfigMapMountPath + "/" + SSHRelativePath
	}

	cmName := sp.cmName(job)
	sshVolume := v1.Volume{
		Name: cmName,
	}
	var mode int32 = 0600
	sshVolume.ConfigMap = &v1.ConfigMapVolumeSource{
		LocalObjectReference: v1.LocalObjectReference{
			Name: cmName,
		},
		Items: []v1.KeyToPath{
			{
				Key:  SSHPrivateKey,
				Path: SSHRelativePath + "/" + SSHPrivateKey,
			},
			{
				Key:  SSHPublicKey,
				Path: SSHRelativePath + "/" + SSHPublicKey,
			},
			{
				Key:  SSHPublicKey,
				Path: SSHRelativePath + "/" + SSHAuthorizedKeys,
			},
			{
				Key:  SSHConfig,
				Path: SSHRelativePath + "/" + SSHConfig,
			},
		},
		DefaultMode: &mode,
	}

	if sshPath != SSHAbsolutePath {
		var noRootMode int32 = 0755
		sshVolume.ConfigMap.DefaultMode = &noRootMode
	}

	pod.Spec.Volumes = append(pod.Spec.Volumes, sshVolume)

	for i, c := range pod.Spec.Containers {
		vm := v1.VolumeMount{
			MountPath: sshPath,
			SubPath:   SSHRelativePath,
			Name:      cmName,
		}

		pod.Spec.Containers[i].VolumeMounts = append(c.VolumeMounts, vm)
	}

	return
}

func generateRsaKey() (map[string]string, error) {
	bitSize := 1024

	privateKey, err := rsa.GenerateKey(rand.Reader, bitSize)
	if err != nil {
		glog.Errorf("rsa generateKey err: %v", err)
		return nil, err
	}

	// id_rsa
	privBlock := pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(privateKey),
	}
	privateKeyBytes := pem.EncodeToMemory(&privBlock)

	// id_rsa.pub
	publicRsaKey, err := ssh.NewPublicKey(&privateKey.PublicKey)
	if err != nil {
		glog.Errorf("ssh newPublicKey err: %v", err)
		return nil, err
	}
	publicKeyBytes := ssh.MarshalAuthorizedKey(publicRsaKey)

	data := make(map[string]string)
	data[SSHPrivateKey] = string(privateKeyBytes)
	data[SSHPublicKey] = string(publicKeyBytes)
	data[SSHConfig] = "StrictHostKeyChecking no\nUserKnownHostsFile /dev/null"

	return data, nil
}

func (sp *sshPlugin) cmName(job *vkv1.Job) string {
	return fmt.Sprintf("%s-%s", job.Name, sp.Name())
}

func (sp *sshPlugin) addFlags() {
	flagSet := flag.NewFlagSet(sp.Name(), flag.ContinueOnError)
	flagSet.BoolVar(&sp.noRoot, "no-root", sp.noRoot, "The ssh user, --no-root is common user")

	if err := flagSet.Parse(sp.pluginArguments); err != nil {
		glog.Errorf("plugin %s flagset parse failed, err: %v", sp.Name(), err)
	}
	return
}
