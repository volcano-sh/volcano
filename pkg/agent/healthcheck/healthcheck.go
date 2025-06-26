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

package healthcheck

import (
	"net/http"

	"k8s.io/klog/v2"

	"volcano.sh/volcano/pkg/networkqos"
)

type HealthChecker interface {
	HealthCheck(w http.ResponseWriter, r *http.Request)
}

type healthChecker struct {
	networkQoSMgr networkqos.NetworkQoSManager
}

func NewHealthChecker(networkQoSMgr networkqos.NetworkQoSManager) HealthChecker {
	return &healthChecker{
		networkQoSMgr: networkQoSMgr,
	}
}

func (c *healthChecker) HealthCheck(w http.ResponseWriter, r *http.Request) {
	if c.networkQoSMgr != nil {
		if err := c.networkQoSMgr.HealthCheck(); err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			if _, writeErr := w.Write([]byte(err.Error())); writeErr != nil {
				klog.ErrorS(writeErr, "Failed to check network qos")
			}
			klog.ErrorS(err, "Failed to check volcano-agent")
			return
		}
	}

	w.WriteHeader(http.StatusOK)
	if _, writeErr := w.Write([]byte(`ok`)); writeErr != nil {
		klog.ErrorS(writeErr, "Failed to write ok to response")
	} else {
		klog.V(3).InfoS("successfully checking health of volcano-agent ")
	}
}
