/*
Copyright 2024 The Volcano Authors.

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

package api

import (
	"net"

	"github.com/containernetworking/cni/pkg/types"
)

// K8sArgs is the valid CNI_ARGS used for Kubernetes
type K8sArgs struct {
	types.CommonArgs

	// IP is pod's ip address
	IP net.IP

	// K8S_POD_NAME is pod's name
	K8S_POD_NAME types.UnmarshallableString

	// K8S_POD_NAMESPACE is pod's namespace
	K8S_POD_NAMESPACE types.UnmarshallableString

	// K8S_POD_INFRA_CONTAINER_ID is pod's container id
	K8S_POD_INFRA_CONTAINER_ID types.UnmarshallableString

	// internal
	// K8S_POD_UID is pod's UID
	K8S_POD_UID types.UnmarshallableString

	// K8S_POD_RUNTIME is pod's runtime type
	K8S_POD_RUNTIME types.UnmarshallableString

	// SECURE_CONTAINER is pod's runtime type
	// Deprecated: Use K8S_POD_RUNTIME instead
	SECURE_CONTAINER types.UnmarshallableString
}

// NetConf for cni config file written in json
type NetConf struct {
	types.NetConf `json:",inline"`
	Args          map[string]string `json:"args,omitempty"`
}
