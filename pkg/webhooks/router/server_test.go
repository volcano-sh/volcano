/*
Copyright 2026 The Volcano Authors.

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
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	admissionv1 "k8s.io/api/admission/v1"
)

func TestServeRejectsAdmissionReviewWithoutRequest(t *testing.T) {
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(
		"POST",
		"/validate",
		strings.NewReader(`{"apiVersion":"admission.k8s.io/v1","kind":"AdmissionReview"}`),
	)
	request.Header.Set(CONTENTTYPE, APPLICATIONJSON)

	serve(recorder, request, func(admissionv1.AdmissionReview) *admissionv1.AdmissionResponse {
		t.Fatal("admit function must not be called for a nil request")
		return nil
	})

	response := admissionv1.AdmissionReview{}
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("failed to decode admission response: %v", err)
	}
	if response.Response == nil || response.Response.Allowed {
		t.Fatalf("unexpected admission response: %#v", response.Response)
	}
}

func TestServeAcceptsJSONWithParameters(t *testing.T) {
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(
		http.MethodPost,
		"/validate",
		strings.NewReader(`{"apiVersion":"admission.k8s.io/v1","kind":"AdmissionReview","request":{"uid":"request-1"}}`),
	)
	request.Header.Set(CONTENTTYPE, APPLICATIONJSON+"; charset=utf-8")

	serve(recorder, request, func(admissionv1.AdmissionReview) *admissionv1.AdmissionResponse {
		return &admissionv1.AdmissionResponse{Allowed: true}
	})

	if recorder.Code != http.StatusOK {
		t.Fatalf("response status = %d, want %d", recorder.Code, http.StatusOK)
	}
	if got := recorder.Header().Get(CONTENTTYPE); got != APPLICATIONJSON {
		t.Fatalf("response Content-Type = %q, want %q", got, APPLICATIONJSON)
	}
	response := admissionv1.AdmissionReview{}
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("failed to decode admission response: %v", err)
	}
	if response.Response == nil || !response.Response.Allowed || response.Response.UID != "request-1" {
		t.Fatalf("unexpected admission response: %#v", response.Response)
	}
}

func TestServeRejectsUnsupportedMediaType(t *testing.T) {
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/validate", strings.NewReader("{}"))
	request.Header.Set(CONTENTTYPE, "text/plain")

	serve(recorder, request, func(admissionv1.AdmissionReview) *admissionv1.AdmissionResponse {
		t.Fatal("admit function must not be called for an unsupported media type")
		return nil
	})

	if recorder.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("response status = %d, want %d", recorder.Code, http.StatusUnsupportedMediaType)
	}
}

func TestServeRejectsNonPost(t *testing.T) {
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/validate", nil)
	request.Header.Set(CONTENTTYPE, APPLICATIONJSON)

	serve(recorder, request, func(admissionv1.AdmissionReview) *admissionv1.AdmissionResponse {
		t.Fatal("admit function must not be called for a non-POST request")
		return nil
	})

	if recorder.Code != http.StatusMethodNotAllowed {
		t.Fatalf("response status = %d, want %d", recorder.Code, http.StatusMethodNotAllowed)
	}
	if got := recorder.Header().Get("Allow"); got != http.MethodPost {
		t.Fatalf("Allow header = %q, want %q", got, http.MethodPost)
	}
}
