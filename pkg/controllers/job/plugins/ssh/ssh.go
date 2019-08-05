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

package ssh

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"path"

	"github.com/golang/glog"
	"github.com/spf13/pflag"
	"golang.org/x/crypto/ssh"

	"k8s.io/api/core/v1"

	vkv1 "volcano.sh/volcano/pkg/apis/batch/v1alpha1"
	"volcano.sh/volcano/pkg/apis/helpers"
	vkhelpers "volcano.sh/volcano/pkg/controllers/job/helpers"
	vkinterface "volcano.sh/volcano/pkg/controllers/job/plugins/interface"
)

type sshPlugin struct {
	Clientset vkinterface.PluginClientset

	// flag parse args

	// user allows users to specify any user name existing in the docker image
	user string
}

const defaultUser = "root"

// New creates ssh plugin
func New(client vkinterface.PluginClientset, arguments []string) vkinterface.PluginInterface {
	sshPlugin := sshPlugin{Clientset: client, user: defaultUser}

	sshPlugin.addFlags(arguments)

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

	data, err := generateRsaKey(job)
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
	if sp.user != defaultUser {
		sshPath = path.Join("/home", sp.user, SSHRelativePath)
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

func generateRsaKey(job *vkv1.Job) (map[string]string, error) {
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
	data[SSHConfig] = generateSSHConfig(job)

	return data, nil
}

func (sp *sshPlugin) cmName(job *vkv1.Job) string {
	return fmt.Sprintf("%s-%s", job.Name, sp.Name())
}

func (sp *sshPlugin) addFlags(args []string) {
	flagSet := pflag.NewFlagSet(sp.Name(), pflag.ContinueOnError)
	flagSet.StringVar(&sp.user, "user", sp.user, "The ssh user, for example --user=volcano specifies running as `volcano`.")

	if err := flagSet.Parse(args); err != nil {
		glog.Errorf("plugin %s flagset parse failed, err: %v", sp.Name(), err)
	}
	return
}

func generateSSHConfig(job *vkv1.Job) string {
	config := "StrictHostKeyChecking no\nUserKnownHostsFile /dev/null\n"

	for _, ts := range job.Spec.Tasks {
		for i := 0; i < int(ts.Replicas); i++ {
			hostName := ts.Template.Spec.Hostname
			subdomain := ts.Template.Spec.Subdomain
			if len(hostName) == 0 {
				hostName = vkhelpers.MakePodName(job.Name, ts.Name, i)
			}
			if len(subdomain) == 0 {
				subdomain = job.Name
			}

			config += "Host " + hostName + "\n"
			config += "  HostName " + hostName + "." + subdomain + "\n"
			if len(ts.Template.Spec.Hostname) != 0 {
				break
			}
		}
	}

	return config
}
