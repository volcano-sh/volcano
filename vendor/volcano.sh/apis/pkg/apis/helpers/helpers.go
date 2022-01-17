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

package helpers

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"reflect"
	"syscall"
	"time"

	v1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apiserver/pkg/server/healthz"
	"k8s.io/apiserver/pkg/server/mux"
	"k8s.io/client-go/kubernetes"
	"k8s.io/klog"

	vcbatch "volcano.sh/apis/pkg/apis/batch/v1alpha1"
	vcbus "volcano.sh/apis/pkg/apis/bus/v1alpha1"
	schedulerv1beta1 "volcano.sh/apis/pkg/apis/scheduling/v1beta1"
)

// JobKind creates job GroupVersionKind.
var JobKind = vcbatch.SchemeGroupVersion.WithKind("Job")

// CommandKind creates command GroupVersionKind.
var CommandKind = vcbus.SchemeGroupVersion.WithKind("Command")

// V1beta1QueueKind is queue kind with v1alpha2 version.
var V1beta1QueueKind = schedulerv1beta1.SchemeGroupVersion.WithKind("Queue")

// CreateOrUpdateConfigMap creates config map if not present or updates config map if necessary.
func CreateOrUpdateConfigMap(job *vcbatch.Job, kubeClients kubernetes.Interface, data map[string]string, cmName string) error {
	// If ConfigMap does not exist, create one for Job.
	cmOld, err := kubeClients.CoreV1().ConfigMaps(job.Namespace).Get(context.TODO(), cmName, metav1.GetOptions{})
	if err != nil {
		if !apierrors.IsNotFound(err) {
			klog.V(3).Infof("Failed to get ConfigMap for Job <%s/%s>: %v",
				job.Namespace, job.Name, err)
			return err
		}

		cm := &v1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: job.Namespace,
				Name:      cmName,
				OwnerReferences: []metav1.OwnerReference{
					*metav1.NewControllerRef(job, JobKind),
				},
			},
			Data: data,
		}

		if _, err := kubeClients.CoreV1().ConfigMaps(job.Namespace).Create(context.TODO(), cm, metav1.CreateOptions{}); err != nil {
			klog.V(3).Infof("Failed to create ConfigMap for Job <%s/%s>: %v",
				job.Namespace, job.Name, err)
			return err
		}
		return nil
	}

	// no changes
	if reflect.DeepEqual(cmOld.Data, data) {
		return nil
	}

	cmOld.Data = data
	if _, err := kubeClients.CoreV1().ConfigMaps(job.Namespace).Update(context.TODO(), cmOld, metav1.UpdateOptions{}); err != nil {
		klog.V(3).Infof("Failed to update ConfigMap for Job <%s/%s>: %v",
			job.Namespace, job.Name, err)
		return err
	}

	return nil
}

// CreateOrUpdateSecret creates secret if not present or updates secret if necessary
func CreateOrUpdateSecret(job *vcbatch.Job, kubeClients kubernetes.Interface, data map[string][]byte, secretName string) error {
	secretOld, err := kubeClients.CoreV1().Secrets(job.Namespace).Get(context.TODO(), secretName, metav1.GetOptions{})
	if err != nil {
		if !apierrors.IsNotFound(err) {
			klog.V(3).Infof("Failed to get Secret for Job <%s/%s>: %v",
				job.Namespace, job.Name, err)
			return err
		}

		secret := &v1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      secretName,
				Namespace: job.Namespace,
				OwnerReferences: []metav1.OwnerReference{
					*metav1.NewControllerRef(job, JobKind),
				},
			},
			Data: data,
		}

		if _, err := kubeClients.CoreV1().Secrets(job.Namespace).Create(context.TODO(), secret, metav1.CreateOptions{}); err != nil {
			klog.V(3).Infof("Failed to create Secret for Job <%s/%s>: %v",
				job.Namespace, job.Name, err)
			return err
		}

		return nil
	}

	// no changes
	SSHConfig := "config"
	if reflect.DeepEqual(secretOld.Data[SSHConfig], data[SSHConfig]) {
		return nil
	}

	secretOld.Data = data
	if _, err := kubeClients.CoreV1().Secrets(job.Namespace).Update(context.TODO(), secretOld, metav1.UpdateOptions{}); err != nil {
		klog.V(3).Infof("Failed to update Secret for Job <%s/%s>: %v",
			job.Namespace, job.Name, err)
		return err
	}

	return nil
}

// DeleteConfigmap deletes the config map resource.
func DeleteConfigmap(job *vcbatch.Job, kubeClients kubernetes.Interface, cmName string) error {
	if err := kubeClients.CoreV1().ConfigMaps(job.Namespace).Delete(context.TODO(), cmName, metav1.DeleteOptions{}); err != nil && !apierrors.IsNotFound(err) {
		klog.Errorf("Failed to delete Configmap of Job %v/%v: %v",
			job.Namespace, job.Name, err)
		return err
	}

	return nil
}

// DeleteSecret delete secret.
func DeleteSecret(job *vcbatch.Job, kubeClients kubernetes.Interface, secretName string) error {
	err := kubeClients.CoreV1().Secrets(job.Namespace).Delete(context.TODO(), secretName, metav1.DeleteOptions{})
	if err != nil && apierrors.IsNotFound(err) {
		return nil
	}

	return err
}

// GeneratePodgroupName generate podgroup name of normal pod.
func GeneratePodgroupName(pod *v1.Pod) string {
	pgName := vcbatch.PodgroupNamePrefix

	if len(pod.OwnerReferences) != 0 {
		for _, ownerReference := range pod.OwnerReferences {
			if ownerReference.Controller != nil && *ownerReference.Controller {
				pgName += string(ownerReference.UID)
				return pgName
			}
		}
	}

	pgName += string(pod.UID)

	return pgName
}

// StartHealthz register healthz interface.
func StartHealthz(healthzBindAddress, name string) error {
	listener, err := net.Listen("tcp", healthzBindAddress)
	if err != nil {
		return fmt.Errorf("failed to create listener: %v", err)
	}

	pathRecorderMux := mux.NewPathRecorderMux(name)
	healthz.InstallHandler(pathRecorderMux)

	server := &http.Server{
		Addr:           listener.Addr().String(),
		Handler:        pathRecorderMux,
		MaxHeaderBytes: 1 << 20,
	}

	return runServer(server, listener)
}

func runServer(server *http.Server, ln net.Listener) error {
	if ln == nil || server == nil {
		return fmt.Errorf("listener and server must not be nil")
	}

	stopCh := make(chan os.Signal)
	signal.Notify(stopCh, syscall.SIGTERM, syscall.SIGINT)

	go func() {
		<-stopCh
		ctx, cancel := context.WithTimeout(context.Background(), 0)
		server.Shutdown(ctx)
		cancel()
	}()

	go func() {
		defer utilruntime.HandleCrash()

		listener := tcpKeepAliveListener{ln.(*net.TCPListener)}

		err := server.Serve(listener)
		msg := fmt.Sprintf("Stopped listening on %s", listener.Addr().String())
		select {
		case <-stopCh:
			klog.Info(msg)
		default:
			klog.Fatalf("%s due to error: %v", msg, err)
		}
	}()

	return nil
}

type tcpKeepAliveListener struct {
	*net.TCPListener
}

// Accept waits for and returns the next connection to the listener.
func (ln tcpKeepAliveListener) Accept() (net.Conn, error) {
	tc, err := ln.AcceptTCP()
	if err != nil {
		return nil, err
	}
	tc.SetKeepAlive(true)
	tc.SetKeepAlivePeriod(3 * time.Minute)
	return tc, nil
}
