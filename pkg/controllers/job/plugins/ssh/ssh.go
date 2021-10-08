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
	"flag"
	"fmt"

	"golang.org/x/crypto/ssh"
	v1 "k8s.io/api/core/v1"
	"k8s.io/klog"

	batch "volcano.sh/apis/pkg/apis/batch/v1alpha1"
	"volcano.sh/apis/pkg/apis/helpers"
	jobhelpers "volcano.sh/volcano/pkg/controllers/job/helpers"
	pluginsinterface "volcano.sh/volcano/pkg/controllers/job/plugins/interface"
)

type sshPlugin struct {
	// Arguments given for the plugin
	pluginArguments []string

	client pluginsinterface.PluginClientset

	// flag parse args
	sshKeyFilePath string

	// private key string
	sshPrivateKey string

	// public key string
	sshPublicKey string
}

// New creates ssh plugin
func New(client pluginsinterface.PluginClientset, arguments []string) pluginsinterface.PluginInterface {
	p := sshPlugin{
		pluginArguments: arguments,
		client:          client,
		sshKeyFilePath:  SSHAbsolutePath,
	}

	p.addFlags()

	return &p
}

func (sp *sshPlugin) Name() string {
	return "ssh"
}

func (sp *sshPlugin) OnPodCreate(pod *v1.Pod, job *batch.Job) error {
	sp.mountRsaKey(pod, job)

	return nil
}

func (sp *sshPlugin) OnJobAdd(job *batch.Job) error {
	if job.Status.ControlledResources["plugin-"+sp.Name()] == sp.Name() {
		return nil
	}

	var data map[string][]byte
	var err error
	if len(sp.sshPrivateKey) > 0 {
		data, err = withUserProvidedRsaKey(job, sp.sshPrivateKey, sp.sshPublicKey)
	} else {
		data, err = generateRsaKey(job)
	}
	if err != nil {
		return err
	}

	if err := helpers.CreateOrUpdateSecret(job, sp.client.KubeClients, data, sp.secretName(job)); err != nil {
		return fmt.Errorf("create secret for job <%s/%s> with ssh plugin failed for %v",
			job.Namespace, job.Name, err)
	}

	job.Status.ControlledResources["plugin-"+sp.Name()] = sp.Name()

	return nil
}

func (sp *sshPlugin) OnJobDelete(job *batch.Job) error {
	if job.Status.ControlledResources["plugin-"+sp.Name()] != sp.Name() {
		return nil
	}
	if err := helpers.DeleteSecret(job, sp.client.KubeClients, sp.secretName(job)); err != nil {
		return err
	}
	delete(job.Status.ControlledResources, "plugin-"+sp.Name())

	return nil
}

// TODO: currently a container using a Secret as a subPath volume mount will not receive Secret updates.
// we may not update the job secret due to the above reason now.
// related issue: https://github.com/volcano-sh/volcano/issues/1420
func (sp *sshPlugin) OnJobUpdate(job *batch.Job) error {
	//data, err := generateRsaKey(job)
	//if err != nil {
	//	return err
	//}
	//
	//if err := helpers.CreateOrUpdateSecret(job, sp.client.KubeClients, data, sp.secretName(job)); err != nil {
	//	return fmt.Errorf("update secret for job <%s/%s> with ssh plugin failed for %v",
	//		job.Namespace, job.Name, err)
	//}

	return nil
}

func (sp *sshPlugin) mountRsaKey(pod *v1.Pod, job *batch.Job) {
	secretName := sp.secretName(job)

	sshVolume := v1.Volume{
		Name: secretName,
	}

	var mode int32 = 0600
	sshVolume.Secret = &v1.SecretVolumeSource{
		SecretName: secretName,
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
				Key:  SSHAuthorizedKeys,
				Path: SSHRelativePath + "/" + SSHAuthorizedKeys,
			},
			{
				Key:  SSHConfig,
				Path: SSHRelativePath + "/" + SSHConfig,
			},
		},
		DefaultMode: &mode,
	}

	if sp.sshKeyFilePath != SSHAbsolutePath {
		var noRootMode int32 = 0600
		sshVolume.Secret.DefaultMode = &noRootMode
	}

	pod.Spec.Volumes = append(pod.Spec.Volumes, sshVolume)

	for i, c := range pod.Spec.Containers {
		vm := v1.VolumeMount{
			MountPath: sp.sshKeyFilePath,
			SubPath:   SSHRelativePath,
			Name:      secretName,
		}

		pod.Spec.Containers[i].VolumeMounts = append(c.VolumeMounts, vm)
	}
	for i, c := range pod.Spec.InitContainers {
		vm := v1.VolumeMount{
			MountPath: sp.sshKeyFilePath,
			SubPath:   SSHRelativePath,
			Name:      secretName,
		}

		pod.Spec.InitContainers[i].VolumeMounts = append(c.VolumeMounts, vm)
	}
}

func generateRsaKey(job *batch.Job) (map[string][]byte, error) {
	bitSize := 2048

	privateKey, err := rsa.GenerateKey(rand.Reader, bitSize)
	if err != nil {
		klog.Errorf("rsa generateKey err: %v", err)
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
		klog.Errorf("ssh newPublicKey err: %v", err)
		return nil, err
	}
	publicKeyBytes := ssh.MarshalAuthorizedKey(publicRsaKey)

	data := make(map[string][]byte)
	data[SSHPrivateKey] = privateKeyBytes
	data[SSHPublicKey] = publicKeyBytes
	data[SSHAuthorizedKeys] = publicKeyBytes
	data[SSHConfig] = []byte(generateSSHConfig(job))

	return data, nil
}

func withUserProvidedRsaKey(job *batch.Job, sshPrivateKey string, sshPublicKey string) (map[string][]byte, error) {
	data := make(map[string][]byte)
	data[SSHPrivateKey] = []byte(sshPrivateKey)
	data[SSHPublicKey] = []byte(sshPublicKey)
	data[SSHAuthorizedKeys] = []byte(sshPublicKey)
	data[SSHConfig] = []byte(generateSSHConfig(job))

	return data, nil
}

func (sp *sshPlugin) secretName(job *batch.Job) string {
	return fmt.Sprintf("%s-%s", job.Name, sp.Name())
}

func (sp *sshPlugin) addFlags() {
	flagSet := flag.NewFlagSet(sp.Name(), flag.ContinueOnError)
	flagSet.StringVar(&sp.sshKeyFilePath, "ssh-key-file-path", sp.sshKeyFilePath, "The path used to store "+
		"ssh private and public keys, it is `/root/.ssh` by default.")
	flagSet.StringVar(&sp.sshPrivateKey, "ssh-private-key", sp.sshPrivateKey, "The input string of the private key")
	flagSet.StringVar(&sp.sshPublicKey, "ssh-public-key", sp.sshPublicKey, "The input string of the public key")

	if err := flagSet.Parse(sp.pluginArguments); err != nil {
		klog.Errorf("plugin %s flagset parse failed, err: %v", sp.Name(), err)
	}
}

func generateSSHConfig(job *batch.Job) string {
	config := "StrictHostKeyChecking no\nUserKnownHostsFile /dev/null\n"

	for _, ts := range job.Spec.Tasks {
		for i := 0; i < int(ts.Replicas); i++ {
			hostName := ts.Template.Spec.Hostname
			subdomain := ts.Template.Spec.Subdomain
			if len(hostName) == 0 {
				hostName = jobhelpers.MakePodName(job.Name, ts.Name, i)
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
