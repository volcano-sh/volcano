/*
Copyright 2021 The Volcano Authors.

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
	"k8s.io/apimachinery/pkg/types"

	"volcano.sh/apis/pkg/apis/scheduling"
)

// ClusterID is UID type, serves as unique ID for each queue
type ClusterID types.UID

// SiloClusterInfo will have all details about queue
type SiloClusterInfo struct {
	UID     ClusterID
	Cluster *scheduling.Cluster
}

// NewSiloClusterInfo creates new queueInfo object
func NewSiloClusterInfo(cluster *scheduling.Cluster) *SiloClusterInfo {
	return &SiloClusterInfo{
		UID:     ClusterID(cluster.Name),
		Cluster: cluster,
	}
}
