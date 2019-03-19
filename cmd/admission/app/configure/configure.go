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
package configure

import (
	"encoding/json"
	"flag"
	"fmt"

	"k8s.io/api/admissionregistration/v1beta1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/strategicpatch"
	admissionregistrationv1beta1client "k8s.io/client-go/kubernetes/typed/admissionregistration/v1beta1"
)

// admission-controller server config.
type Config struct {
	Master                    string
	Kubeconfig                string
	CertFile                  string
	KeyFile                   string
	CaCertFile                string
	Port                      int
	MutateWebhookConfigName   string
	MutateWebhookName         string
	ValidateWebhookConfigName string
	ValidateWebhookName       string
}

func NewConfig() *Config {
	c := Config{}
	return &c
}

func (c *Config) AddFlags() {
	flag.StringVar(&c.Master, "master", c.Master, "The address of the Kubernetes API server (overrides any value in kubeconfig)")
	flag.StringVar(&c.Kubeconfig, "kubeconfig", c.Kubeconfig, "Path to kubeconfig file with authorization and master location information.")
	flag.StringVar(&c.CertFile, "tls-cert-file", c.CertFile, ""+
		"File containing the default x509 Certificate for HTTPS. (CA cert, if any, concatenated "+
		"after server cert).")
	flag.StringVar(&c.KeyFile, "tls-private-key-file", c.KeyFile, "File containing the default x509 private key matching --tls-cert-file.")
	flag.StringVar(&c.CaCertFile, "ca-cert-file", c.CaCertFile, "File containing the x509 Certificate for HTTPS.")
	flag.IntVar(&c.Port, "port", 443, "the port used by admission-controller-server.")
	flag.StringVar(&c.MutateWebhookConfigName, "mutate-webhook-config-name", "volcano-mutate-job",
		"Name of the mutatingwebhookconfiguration resource in Kubernetes.")
	flag.StringVar(&c.MutateWebhookName, "mutate-webhook-name", "mutatejob.volcano.sh",
		"Name of the webhook entry in the webhook config.")
	flag.StringVar(&c.ValidateWebhookConfigName, "validate-webhook-config-name", "volcano-validate-job",
		"Name of the mutatingwebhookconfiguration resource in Kubernetes.")
	flag.StringVar(&c.ValidateWebhookName, "validate-webhook-name", "validatejob.volcano.sh",
		"Name of the webhook entry in the webhook config.")
}

func (c *Config) CheckPortOrDie() error {
	if c.Port < 1 || c.Port > 65535 {
		return fmt.Errorf("the port should be in the range of 1 and 65535")
	}
	return nil
}

// PatchMutateWebhookConfig patches a CA bundle into the specified webhook config.
func PatchMutateWebhookConfig(client admissionregistrationv1beta1client.MutatingWebhookConfigurationInterface,
	webhookConfigName, webhookName string, caBundle []byte) error {
	config, err := client.Get(webhookConfigName, metav1.GetOptions{})
	if err != nil {
		return err
	}
	prev, err := json.Marshal(config)
	if err != nil {
		return err
	}
	found := false
	for i, w := range config.Webhooks {
		if w.Name == webhookName {
			config.Webhooks[i].ClientConfig.CABundle = caBundle[:]
			found = true
			break
		}
	}
	if !found {
		return apierrors.NewInternalError(fmt.Errorf(
			"webhook entry %q not found in config %q", webhookName, webhookConfigName))
	}
	curr, err := json.Marshal(config)
	if err != nil {
		return err
	}
	patch, err := strategicpatch.CreateTwoWayMergePatch(prev, curr, v1beta1.MutatingWebhookConfiguration{})
	if err != nil {
		return err
	}

	if string(patch) != "{}" {
		_, err = client.Patch(webhookConfigName, types.StrategicMergePatchType, patch)
	}
	return err
}

// PatchValidateWebhookConfig patches a CA bundle into the specified webhook config.
func PatchValidateWebhookConfig(client admissionregistrationv1beta1client.ValidatingWebhookConfigurationInterface,
	webhookConfigName, webhookName string, caBundle []byte) error {
	config, err := client.Get(webhookConfigName, metav1.GetOptions{})
	if err != nil {
		return err
	}
	prev, err := json.Marshal(config)
	if err != nil {
		return err
	}
	found := false
	for i, w := range config.Webhooks {
		if w.Name == webhookName {
			config.Webhooks[i].ClientConfig.CABundle = caBundle[:]
			found = true
			break
		}
	}
	if !found {
		return apierrors.NewInternalError(fmt.Errorf(
			"webhook entry %q not found in config %q", webhookName, webhookConfigName))
	}
	curr, err := json.Marshal(config)
	if err != nil {
		return err
	}
	patch, err := strategicpatch.CreateTwoWayMergePatch(prev, curr, v1beta1.ValidatingWebhookConfiguration{})
	if err != nil {
		return err
	}

	if string(patch) != "{}" {
		_, err = client.Patch(webhookConfigName, types.StrategicMergePatchType, patch)
	}
	return err
}
