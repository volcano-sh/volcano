/*
Copyright(C)2020-2025. Huawei Technologies Co.,Ltd. All rights reserved.

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

/*
Package k8s is using for the k8s operation.
*/
package k8s

import (
	"time"

	"k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/client-go/kubernetes"
)

var torNodeCache *v1.ConfigMap
var torNodeUpdateTime int64

// GetTorNodeWithOneMinuteDelay get tor node configMap with one-minute delay
func GetTorNodeWithOneMinuteDelay(client kubernetes.Interface, namespace, cmName string) (*v1.ConfigMap, error) {
	if torNodeUpdateTime == 0 || torNodeUpdateTime < time.Now().Unix()-torNodeCacheTime {
		torNode, err := GetConfigMap(client, namespace, cmName)
		if err != nil {
			// basic tor node cm maybe not exists, setting the time in case of errors can reduce unnecessary queries
			torNodeUpdateTime = time.Now().Unix()
			return nil, err
		}
		torNodeCache = torNode
		torNodeUpdateTime = time.Now().Unix()
	}
	if torNodeCache == nil || torNodeCache.Name == "" {
		return nil, errors.NewNotFound(v1.Resource("ConfigMap"), cmName)
	}
	return torNodeCache, nil
}
