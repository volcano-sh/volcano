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
package app

import (
	"crypto/tls"
	"encoding/json"
	"io/ioutil"
	"net/http"

	"github.com/golang/glog"

	"k8s.io/api/admission/v1beta1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes"
	restclient "k8s.io/client-go/rest"

	appConf "volcano.sh/volcano/cmd/admission/app/configure"
	admissioncontroller "volcano.sh/volcano/pkg/admission"
)

const (
	CONTENTTYPE     = "Content-Type"
	APPLICATIONJSON = "application/json"
)

// Get a clientset with restConfig.
func GetClient(restConfig *restclient.Config) *kubernetes.Clientset {
	clientset, err := kubernetes.NewForConfig(restConfig)
	if err != nil {
		glog.Fatal(err)
	}
	return clientset
}

// ConfigTLS is a helper function that generate tls certificates from directly defined tls config or kubeconfig
// These are passed in as command line for cluster certification. If tls config is passed in, we use the directly
// defined tls config, else use that defined in kubeconfig
func ConfigTLS(config *appConf.Config, restConfig *restclient.Config) *tls.Config {
	if len(config.CertFile) != 0 && len(config.KeyFile) != 0 {
		sCert, err := tls.LoadX509KeyPair(config.CertFile, config.KeyFile)
		if err != nil {
			glog.Fatal(err)
		}

		return &tls.Config{
			Certificates: []tls.Certificate{sCert},
		}
	}

	if len(restConfig.CertData) != 0 && len(restConfig.KeyData) != 0 {
		sCert, err := tls.X509KeyPair(restConfig.CertData, restConfig.KeyData)
		if err != nil {
			glog.Fatal(err)
		}

		return &tls.Config{
			Certificates: []tls.Certificate{sCert},
		}
	}

	glog.Fatal("tls: failed to find any tls config data")
	return &tls.Config{}
}

func Serve(w http.ResponseWriter, r *http.Request, admit admissioncontroller.AdmitFunc) {
	var body []byte
	if r.Body != nil {
		if data, err := ioutil.ReadAll(r.Body); err == nil {
			body = data
		}
	}

	// verify the content type is accurate
	contentType := r.Header.Get(CONTENTTYPE)
	if contentType != APPLICATIONJSON {
		glog.Errorf("contentType=%s, expect application/json", contentType)
		return
	}

	var reviewResponse *v1beta1.AdmissionResponse
	ar := v1beta1.AdmissionReview{}
	deserializer := admissioncontroller.Codecs.UniversalDeserializer()
	if _, _, err := deserializer.Decode(body, nil, &ar); err != nil {
		reviewResponse = admissioncontroller.ToAdmissionResponse(err)
	} else {
		reviewResponse = admit(ar)
	}
	glog.V(3).Infof("sending response: %v", reviewResponse)

	response := createResponse(reviewResponse, &ar)
	resp, err := json.Marshal(response)
	if err != nil {
		glog.Error(err)
	}
	if _, err := w.Write(resp); err != nil {
		glog.Error(err)
	}
}

func createResponse(reviewResponse *v1beta1.AdmissionResponse, ar *v1beta1.AdmissionReview) v1beta1.AdmissionReview {
	response := v1beta1.AdmissionReview{}
	if reviewResponse != nil {
		response.Response = reviewResponse
		response.Response.UID = ar.Request.UID
	}
	// reset the Object and OldObject, they are not needed in a response.
	ar.Request.Object = runtime.RawExtension{}
	ar.Request.OldObject = runtime.RawExtension{}

	return response
}
