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
	"encoding/json"
	"io"
	"io/ioutil"
	"net/http"

	admissionv1 "k8s.io/api/admission/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/klog"

	"volcano.sh/volcano/pkg/webhooks/schema"
	"volcano.sh/volcano/pkg/webhooks/util"
)

// CONTENTTYPE http content-type.
var CONTENTTYPE = "Content-Type"

// APPLICATIONJSON json content.
var APPLICATIONJSON = "application/json"

// Serve the http request.
func Serve(w io.Writer, r *http.Request, admit AdmitFunc) {
	var body []byte
	if r.Body != nil {
		if data, err := ioutil.ReadAll(r.Body); err == nil {
			body = data
		}
	}

	// verify the content type is accurate
	contentType := r.Header.Get(CONTENTTYPE)
	if contentType != APPLICATIONJSON {
		klog.Errorf("contentType is not application/json")
		return
	}

	var reviewResponse *admissionv1.AdmissionResponse
	ar := admissionv1.AdmissionReview{}
	deserializer := schema.Codecs.UniversalDeserializer()
	if _, _, err := deserializer.Decode(body, nil, &ar); err != nil {
		reviewResponse = util.ToAdmissionResponse(err)
	} else {
		reviewResponse = admit(ar)
	}
	klog.V(3).Infof("sending response: %v", reviewResponse)

	response := createResponse(reviewResponse, &ar)
	resp, err := json.Marshal(response)
	if err != nil {
		klog.Error(err)
	}
	if _, err := w.Write(resp); err != nil {
		klog.Error(err)
	}
}

func createResponse(reviewResponse *admissionv1.AdmissionResponse, ar *admissionv1.AdmissionReview) admissionv1.AdmissionReview {
	response := admissionv1.AdmissionReview{}
	if reviewResponse != nil {
		response.APIVersion = "admission.k8s.io/v1"
		response.Kind = "AdmissionReview"
		response.Response = reviewResponse
		response.Response.UID = ar.Request.UID
	}
	// reset the Object and OldObject, they are not needed in a response.
	ar.Request.Object = runtime.RawExtension{}
	ar.Request.OldObject = runtime.RawExtension{}

	return response
}
