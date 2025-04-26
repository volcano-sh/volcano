/*
Copyright 2018 The Volcano Authors.

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

package options

import (
	"fmt"
	"os"

	"github.com/spf13/pflag"

	"volcano.sh/volcano/pkg/kube"
)

const (
	defaultSchedulerName    = "volcano"
	defaultQPS              = 50.0
	defaultBurst            = 100
	defaultEnabledAdmission = "/jobs/mutate,/jobs/validate,/podgroups/mutate,/pods/validate,/pods/mutate,/queues/mutate,/queues/validate"
	defaultHealthzAddress   = ":11251"
)

// Config admission-controller server config.
type Config struct {
	KubeClientOptions kube.ClientOptions
	CertFile          string
	KeyFile           string
	CaCertFile        string
	CertData          []byte
	KeyData           []byte
	CaCertData        []byte
	ListenAddress     string
	Port              int
	PrintVersion      bool
	WebhookName       string
	WebhookNamespace  string
	SchedulerNames    []string
	WebhookURL        string
	ConfigPath        string
	EnabledAdmission  string

	EnableHealthz bool
	// HealthzBindAddress is the IP address and port for the health check server to serve on
	// defaulting to :11251
	HealthzBindAddress string
}

type DecryptFunc func(c *Config) error

// NewConfig create new config.
func NewConfig() *Config {
	c := Config{}
	return &c
}

// AddFlags add flags.
func (c *Config) AddFlags(fs *pflag.FlagSet) {
	fs.StringVar(&c.KubeClientOptions.Master, "master", c.KubeClientOptions.Master, "The address of the Kubernetes API server (overrides any value in kubeconfig)")
	fs.StringVar(&c.KubeClientOptions.KubeConfig, "kubeconfig", c.KubeClientOptions.KubeConfig, "Path to kubeconfig file with authorization and master location information.")
	fs.StringVar(&c.CertFile, "tls-cert-file", c.CertFile, ""+
		"File containing the default x509 Certificate for HTTPS. (CA cert, if any, concatenated "+
		"after server cert).")
	fs.StringVar(&c.KeyFile, "tls-private-key-file", c.KeyFile, "File containing the default x509 private key matching --tls-cert-file.")
	fs.StringVar(&c.ListenAddress, "listen-address", "", "The address to listen on for the admission-controller-server.")
	fs.IntVar(&c.Port, "port", 8443, "the port used by admission-controller-server.")
	fs.BoolVar(&c.PrintVersion, "version", false, "Show version and quit")
	fs.Float32Var(&c.KubeClientOptions.QPS, "kube-api-qps", defaultQPS, "QPS to use while talking with kubernetes apiserver")
	fs.IntVar(&c.KubeClientOptions.Burst, "kube-api-burst", defaultBurst, "Burst to use while talking with kubernetes apiserver")
	fs.StringVar(&c.CaCertFile, "ca-cert-file", c.CaCertFile, "File containing the x509 Certificate for HTTPS.")
	fs.StringVar(&c.WebhookNamespace, "webhook-namespace", "", "The namespace of this webhook")
	fs.StringVar(&c.WebhookName, "webhook-service-name", "", "The name of this webhook")
	fs.StringVar(&c.WebhookURL, "webhook-url", "", "The url of this webhook")
	fs.StringVar(&c.EnabledAdmission, "enabled-admission", defaultEnabledAdmission, "enabled admission webhooks, if this parameter is modified, make sure corresponding webhook configurations are the same.")
	fs.StringArrayVar(&c.SchedulerNames, "scheduler-name", []string{defaultSchedulerName}, "Volcano will handle pods whose .spec.SchedulerName is same as scheduler-name")
	fs.StringVar(&c.ConfigPath, "admission-conf", "", "The configmap file of this webhook")
	fs.BoolVar(&c.EnableHealthz, "enable-healthz", false, "Enable the health check; it is false by default")
	fs.StringVar(&c.HealthzBindAddress, "healthz-address", defaultHealthzAddress, "The address to listen on for the health check server.")
}

// CheckPortOrDie check valid port range.
func (c *Config) CheckPortOrDie() error {
	if c.Port < 1 || c.Port > 65535 {
		return fmt.Errorf("the port should be in the range of 1 and 65535")
	}
	return nil
}

// readCAFiles read data from ca file path
func (c *Config) readCAFiles() error {
	var err error
	c.CaCertData, err = os.ReadFile(c.CaCertFile)
	if err != nil {
		return fmt.Errorf("failed to read cacert file (%s): %v", c.CaCertFile, err)
	}

	c.CertData, err = os.ReadFile(c.CertFile)
	if err != nil {
		return fmt.Errorf("failed to read cert file (%s): %v", c.CertFile, err)
	}

	c.KeyData, err = os.ReadFile(c.KeyFile)
	if err != nil {
		return fmt.Errorf("failed to read key file (%s): %v", c.KeyFile, err)
	}

	return nil
}

// ParseCAFiles parse ca file by decryptFunc
func (c *Config) ParseCAFiles(decryptFunc DecryptFunc) error {
	if err := c.readCAFiles(); err != nil {
		return err
	}

	// users can add one function to decrypt the data by their own way if CA data is encrypted
	if decryptFunc != nil {
		return decryptFunc(c)
	}

	return nil
}
