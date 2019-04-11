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

const (
	// SSHPrivateKey is private key file name
	SSHPrivateKey = "id_rsa"
	// SSHPublicKey is public key file name
	SSHPublicKey = "id_rsa.pub"
	// SSHAuthorizedKeys is authorized key file name
	SSHAuthorizedKeys = "authorized_keys"
	// SSHConfig is ssh config file name
	SSHConfig = "config"

	// SSHAbsolutePath absolute path for ssh folder
	SSHAbsolutePath = "/root/.ssh"
	// SSHRelativePath relative path for ssh folder
	SSHRelativePath = ".ssh"
)
