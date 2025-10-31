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
	"context"
	"fmt"
	"reflect"
	"strings"
	"time"

	"k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/klog/v2"

	"volcano.sh/volcano/third_party/ascend-for-volcano/common/util"
)

// GetConfigMapWithRetry  Get config map from k8s.
func GetConfigMapWithRetry(client kubernetes.Interface, namespace, cmName string) (*v1.ConfigMap, error) {
	var cm *v1.ConfigMap
	var err error

	for i := 0; i < retryTime; i++ {
		// There can be no delay or blocking operations in a session.
		if cm, err = client.CoreV1().ConfigMaps(namespace).Get(context.TODO(), cmName, metav1.GetOptions{}); err != nil {
			time.Sleep(retrySleepTime)
			continue
		}
		return cm, nil
	}
	return nil, err
}

// GetConfigMap  Get config map from k8s.
func GetConfigMap(client kubernetes.Interface, namespace, cmName string) (*v1.ConfigMap, error) {
	var cm *v1.ConfigMap
	var err error
	// There can be no delay or blocking operations in a session.
	if cm, err = client.CoreV1().ConfigMaps(namespace).Get(context.TODO(), cmName, metav1.GetOptions{}); err != nil {
		return nil, err
	}
	return cm, nil
}

// IsConfigMapChanged judge the cm wither is same. true is no change.
func IsConfigMapChanged(k8s kubernetes.Interface, cm *v1.ConfigMap, cmName, nameSpace string) bool {
	cmData, getErr := GetConfigMapWithRetry(k8s, nameSpace, cmName)
	if getErr != nil {
		return true
	}
	if reflect.DeepEqual(cmData, cm) {
		return false
	}

	return true
}

// CreateOrUpdateConfigMap Create or update configMap.
func CreateOrUpdateConfigMap(k8s kubernetes.Interface, cm *v1.ConfigMap, cmName, nameSpace string) error {
	klog.V(util.LogDebugLev).Infof("cmName: %s, cmNamespace: %s", cmName, cm.ObjectMeta.Namespace)
	_, cErr := k8s.CoreV1().ConfigMaps(cm.ObjectMeta.Namespace).Create(context.TODO(), cm, metav1.CreateOptions{})
	if cErr != nil {
		if !errors.IsAlreadyExists(cErr) {
			return fmt.Errorf("unable to create ConfigMap:%s", util.SafePrint(cErr))
		}

		// To reduce the cm write operations
		if !IsConfigMapChanged(k8s, cm, cmName, nameSpace) {
			klog.V(util.LogInfoLev).Infof("configMap not changed,no need update")
			return nil
		}

		_, err := k8s.CoreV1().ConfigMaps(cm.ObjectMeta.Namespace).Update(context.TODO(), cm, metav1.UpdateOptions{})
		if err != nil {
			return fmt.Errorf("unable to update ConfigMap:%s", util.SafePrint(cErr))
		}
	}
	return nil
}

// UpdateConfigmapIncrementally update configmap Map data but keep the key value pair that new data does not have
func UpdateConfigmapIncrementally(kubeClient kubernetes.Interface, ns, name string,
	newData map[string]string) (map[string]string, error) {
	if len(newData) == 0 {
		return newData, fmt.Errorf("newData is empty")
	}
	oldCM, err := GetConfigMapWithRetry(kubeClient, ns, name)
	if err != nil || oldCM == nil {
		upCmErr := fmt.Errorf("get old configmap from kubernetes failed err:%s", util.SafePrint(err))
		return newData, upCmErr

	}
	oldCMData := oldCM.Data
	if oldCMData != nil {
		for key, value := range oldCMData {
			_, ok := newData[key]
			if !ok {
				newData[key] = value // place the key-value pairs from kubernetes back
				continue
			}
		}
	}
	return newData, nil
}

// InformerConfigmapFilter is used to filter out cm need to be listened for ascend plugin
func InformerConfigmapFilter(obj interface{}) bool {
	cm, ok := obj.(*v1.ConfigMap)
	if !ok {
		klog.V(util.LogErrorLev).Infof("Cannot convert to ConfigMap:%#v", obj)
		return false
	}
	return CheckConfigMapIsDeviceInfo(cm) || CheckConfigMapIsNodeInfo(cm)
}

// CheckConfigMapIsDeviceInfo check configmap is device info
func CheckConfigMapIsDeviceInfo(cm *v1.ConfigMap) bool {
	return cm.Namespace == util.DevInfoNameSpace && strings.HasPrefix(cm.Name, util.DevInfoPreName)
}

// CheckConfigMapIsNodeInfo check whether the configmap is kube-system/node-info-
func CheckConfigMapIsNodeInfo(cm *v1.ConfigMap) bool {
	return cm.Namespace == util.MindXDlNameSpace && strings.HasPrefix(cm.Name, util.NodeDCmInfoNamePrefix)
}
