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
package configure

import (
	"flag"
	"fmt"
)

// admission-controller server config.
type Config struct {
	Master     string
	Kubeconfig string
	CertFile   string
	KeyFile    string
	Port       int
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
	flag.IntVar(&c.Port, "port", 443, "the port used by admission-controller-server.")
}

func (c *Config) CheckPortOrDie() error {
	if c.Port < 1 || c.Port > 65535 {
		return fmt.Errorf("the port should be in the range of 1 and 65535")
	}
	return nil
}
