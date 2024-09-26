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

package router

import (
	"fmt"
	"net/http"
	"strings"
	"sync"

	"k8s.io/klog/v2"

	"volcano.sh/volcano/cmd/webhook-manager/app/options"
)

type AdmissionHandler func(w http.ResponseWriter, r *http.Request)

var admissionMap = make(map[string]*AdmissionService)
var admissionMutex sync.Mutex

func RegisterAdmission(service *AdmissionService) error {
	admissionMutex.Lock()
	defer admissionMutex.Unlock()

	if _, found := admissionMap[service.Path]; found {
		return fmt.Errorf("duplicated admission service for %s", service.Path)
	}

	// Also register handler to the service.
	service.Handler = func(w http.ResponseWriter, r *http.Request) {
		Serve(w, r, service.Func)
	}

	admissionMap[service.Path] = service

	return nil
}

func ForEachAdmission(config *options.Config, handler func(*AdmissionService) error) error {
	admissions := strings.Split(strings.TrimSpace(config.EnabledAdmission), ",")
	klog.V(3).Infof("Enabled admissions are: %v, registered map are: %v", admissions, admissionMap)
	for _, admission := range admissions {
		if service, found := admissionMap[admission]; found {
			if err := handler(service); err != nil {
				return err
			}
		} else {
			return fmt.Errorf("enabled admission %s not found on the admission registered map", admission)
		}
	}
	return nil
}
