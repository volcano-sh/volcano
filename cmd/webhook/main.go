/*
Copyright 2018 The Vulcan Authors.

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
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io/ioutil"
	"net/http"
	"strings"
	reflect "reflect"

	"github.com/golang/glog"
	"k8s.io/api/admission/v1beta1"
	v1alpha1 "hpw.cloud/volcano/pkg/apis/batch/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

// Config contains the server (the webhook) cert and key.
type Config struct {
	CertFile string
	KeyFile  string
}

func (c *Config) addFlags() {
	flag.StringVar(&c.CertFile, "tls-cert-file", c.CertFile, ""+
		"File containing the default x509 Certificate for HTTPS. (CA cert, if any, concatenated "+
		"after server cert).")
	flag.StringVar(&c.KeyFile, "tls-private-key-file", c.KeyFile, ""+
		"File containing the default x509 private key matching --tls-cert-file.")
}

func toAdmissionResponse(err error) *v1beta1.AdmissionResponse {
	return &v1beta1.AdmissionResponse{
		Result: &metav1.Status{
			Message: err.Error(),
		},
	}
}

// job admit.
func admitJobs(ar v1beta1.AdmissionReview) *v1beta1.AdmissionResponse {
	glog.V(3).Info("admitting jobs")
	jobResource := metav1.GroupVersionResource{Group: v1alpha1.SchemeGroupVersion.Group, Version: v1alpha1.SchemeGroupVersion.Version, Resource: "jobs"}
	if ar.Request.Resource != jobResource {
		err := fmt.Errorf("expect resource to be %s", jobResource)
		glog.Error(err)
		return toAdmissionResponse(err)
	}

	raw := ar.Request.Object.Raw
	job := v1alpha1.Job{}


	deserializer := codecs.UniversalDeserializer()
	if _, _, err := deserializer.Decode(raw, nil, &job); err != nil {
		glog.Error(err)
		return toAdmissionResponse(err)
	}

	reviewResponse := v1beta1.AdmissionResponse{}
	reviewResponse.Allowed = true

	tip := fmt.Errorf( "the job struct is %+v", job) 
	glog.V(3).Info(tip)

	var msg string

	var taskPlicyEvents []v1alpha1.Event
	var jobPlicyEvents []v1alpha1.Event
	var taskNames []string

	var totalReplicas int32
	totalReplicas = 0

	minAvailable := job.Spec.MinAvailable
	for _, task := range job.Spec.Tasks {

		// count replicas
		totalReplicas = totalReplicas + task.Replicas

		// duplicate task name 
		if ok := Contain(task.Name, taskNames); ok{
			reviewResponse.Allowed = false
			msg = msg + "duplicated task name"
			break
		}else{
			taskNames = append(taskNames, task.Name)
		}

		//duplicate task policies event
		for _, taskPolicy := range task.Policies {
			if ok := Contain(taskPolicy.Event, taskPlicyEvents); ok{
				reviewResponse.Allowed = false
				msg = msg + "duplicated task policies event"
				break
			}else{
				taskPlicyEvents = append(taskPlicyEvents, taskPolicy.Event)
			}
		}
	}

	if totalReplicas < minAvailable {
		reviewResponse.Allowed = false;
		msg = msg + "minAvailable should not be larger than total replicas in tasks"
	}

	//duplicate job policies event
	for _, jobPolicy := range job.Spec.Policies {
		if ok := Contain(jobPolicy.Event, jobPlicyEvents); ok{
			reviewResponse.Allowed = false
			msg = msg + "duplicated job policies event"
			break
		}else{
			jobPlicyEvents = append(jobPlicyEvents, jobPolicy.Event)
		}
	}

	if !reviewResponse.Allowed {
		reviewResponse.Result = &metav1.Status{Message: strings.TrimSpace(msg)}
	}
	return &reviewResponse
}

func Contain(obj interface{}, target interface{}) (bool) {
    targetValue := reflect.ValueOf(target)
    switch reflect.TypeOf(target).Kind() {
    case reflect.Slice, reflect.Array:
        for i := 0; i < targetValue.Len(); i++ {
            if targetValue.Index(i).Interface() == obj {
                return true
            }
        }
    case reflect.Map:
        if targetValue.MapIndex(reflect.ValueOf(obj)).IsValid() {
            return true
        }
    }

    return false
}

type admitFunc func(v1beta1.AdmissionReview) *v1beta1.AdmissionResponse

func serve(w http.ResponseWriter, r *http.Request, admit admitFunc) {
	var body []byte
	if r.Body != nil {
		if data, err := ioutil.ReadAll(r.Body); err == nil {
			body = data
		}
	}

	// verify the content type is accurate
	contentType := r.Header.Get("Content-Type")
	if contentType != "application/json" {
		glog.Errorf("contentType=%s, expect application/json", contentType)
		return
	}

	glog.V(3).Info(fmt.Sprintf("handling request: %v", body))
	var reviewResponse *v1beta1.AdmissionResponse
	ar := v1beta1.AdmissionReview{}
	deserializer := codecs.UniversalDeserializer()
	if _, _, err := deserializer.Decode(body, nil, &ar); err != nil {
		glog.Error(err)
		reviewResponse = toAdmissionResponse(err)
	} else {
		reviewResponse = admit(ar)
	}
	glog.V(3).Info(fmt.Sprintf("sending response: %v", reviewResponse))

	response := v1beta1.AdmissionReview{}
	if reviewResponse != nil {
		response.Response = reviewResponse
		response.Response.UID = ar.Request.UID
	}
	// reset the Object and OldObject, they are not needed in a response.
	ar.Request.Object = runtime.RawExtension{}
	ar.Request.OldObject = runtime.RawExtension{}

	resp, err := json.Marshal(response)
	if err != nil {
		glog.Error(err)
	}
	if _, err := w.Write(resp); err != nil {
		glog.Error(err)
	}
}

func serveJobs(w http.ResponseWriter, r *http.Request) {
	serve(w, r, admitJobs)
}


func main() {
	var config Config
	config.addFlags()
	flag.Parse()

	http.HandleFunc("/jobs", serveJobs)

	clientset := getClient()
	server := &http.Server{
		Addr:      ":443",
		TLSConfig: configTLS(config, clientset),
	}
	server.ListenAndServeTLS("", "")
}