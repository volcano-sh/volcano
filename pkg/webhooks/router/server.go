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
	"fmt"
	"io"
	"mime"
	"net/http"

	admissionv1 "k8s.io/api/admission/v1"
	"k8s.io/klog/v2"

	"volcano.sh/volcano/pkg/webhooks/schema"
	"volcano.sh/volcano/pkg/webhooks/util"
)

// CONTENTTYPE http content-type.
var CONTENTTYPE = "Content-Type"

// APPLICATIONJSON json content.
var APPLICATIONJSON = "application/json"

// MaxRequestBody caps the admission request body size to avoid OOM from
// oversized requests. 3 MiB matches the kube-apiserver default.
const MaxRequestBody int64 = 3 * 1024 * 1024

// serve the http request.
func serve(w http.ResponseWriter, r *http.Request, admit AdmitFunc) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	contentType, _, err := mime.ParseMediaType(r.Header.Get(CONTENTTYPE))
	if err != nil || contentType != APPLICATIONJSON {
		http.Error(w, "unsupported media type", http.StatusUnsupportedMediaType)
		return
	}

	var body []byte
	if r.Body != nil {
		r.Body = http.MaxBytesReader(w, r.Body, MaxRequestBody)
		data, err := io.ReadAll(r.Body)
		if err != nil {
			klog.Errorf("Failed to read admission request body: %v", err)
			http.Error(w, "request body too large", http.StatusRequestEntityTooLarge)
			return
		}
		body = data
	}

	var reviewResponse *admissionv1.AdmissionResponse
	ar := admissionv1.AdmissionReview{}
	deserializer := schema.Codecs.UniversalDeserializer()
	if _, _, err := deserializer.Decode(body, nil, &ar); err != nil {
		reviewResponse = util.ToAdmissionResponse(err)
	} else if ar.Request == nil {
		reviewResponse = util.ToAdmissionResponse(
			fmt.Errorf("admission review request must not be nil"),
		)
	} else {
		reviewResponse = admit(ar)
	}
	klog.V(5).Infof("sending response: %v", reviewResponse)

	response := createResponse(reviewResponse, &ar)
	resp, err := json.Marshal(response)
	if err != nil {
		http.Error(w, "failed to encode admission response", http.StatusInternalServerError)
		return
	}
	w.Header().Set(CONTENTTYPE, APPLICATIONJSON)
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
		if ar != nil && ar.Request != nil {
			response.Response.UID = ar.Request.UID
		}
	}

	return response
}
