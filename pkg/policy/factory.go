/*
Copyright 2017 The Kubernetes Authors.

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

package policy

import "sync"

var policyMap = make(map[string]Interface)
var mutex sync.Mutex

func New(name string) Interface {
	mutex.Lock()
	defer mutex.Unlock()

	policy, found := policyMap[name]
	if !found {
		return nil
	}

	return policy
}

func RegisterPolicy(name string, policy Interface) error {
	mutex.Lock()
	defer mutex.Unlock()

	policyMap[name] = policy

	return nil
}

func RemovePolicy(name string) error {
	mutex.Lock()
	defer mutex.Unlock()

	delete(policyMap, name)

	return nil
}
