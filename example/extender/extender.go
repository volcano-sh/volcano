/*
Copyright 2022 The Volcano Authors.

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
	"io/ioutil"
	"net/http"
	"volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/plugins/extender"
)

var snapshot *api.ClusterInfo

func onSessionOpen(w http.ResponseWriter, r *http.Request) {
	content, err := ioutil.ReadAll(r.Body)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	r.Body.Close()

	req := &extender.OnSessionOpenRequest{}
	if err := json.Unmarshal(content, req); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	snapshot = &api.ClusterInfo{
		Jobs:           req.Jobs,
		Nodes:          req.Nodes,
		Queues:         req.Queues,
		NamespaceInfo:  req.NamespaceInfo,
		RevocableNodes: req.RevocableNodes,
	}
}

func onSessionClose(w http.ResponseWriter, r *http.Request) {
	snapshot = nil
}

func predicate(w http.ResponseWriter, r *http.Request) {
	content, err := ioutil.ReadAll(r.Body)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	r.Body.Close()

	req := &extender.PredicateRequest{}
	if err := json.Unmarshal(content, req); err != nil || req.Task == nil || req.Node == nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	resp := &extender.PredicateResponse{}
	if req.Task.BestEffort && len(req.Node.Tasks) > 10 {
		resp.ErrorMessage = "Too many tasks on the node"
	}
	response, err := json.Marshal(resp)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	if _, err = w.Write(response); err != nil {
		w.WriteHeader(http.StatusInternalServerError)
	}
}

func prioritize(w http.ResponseWriter, r *http.Request) {
	content, err := ioutil.ReadAll(r.Body)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	r.Body.Close()

	req := &extender.PrioritizeRequest{}
	if err := json.Unmarshal(content, req); err != nil || req.Task == nil || len(req.Nodes) == 0 {
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	resp := &extender.PrioritizeResponse{NodeScore: map[string]float64{}}
	for i := range req.Nodes {
		if req.Task.BestEffort && len(req.Nodes[i].Tasks) > 5 {
			resp.NodeScore[req.Nodes[i].Name] = 0
		} else {
			resp.NodeScore[req.Nodes[i].Name] = 1
		}
	}

	response, err := json.Marshal(resp)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	if _, err = w.Write(response); err != nil {
		w.WriteHeader(http.StatusInternalServerError)
	}
}

func main() {
	http.HandleFunc("/onSessionOpen", onSessionOpen)
	http.HandleFunc("/onSessionClose", onSessionClose)
	http.HandleFunc("/predicate", predicate)
	http.HandleFunc("/prioritize", prioritize)

	if err := http.ListenAndServe(":8713", nil); err != nil {
		panic(err)
	}
}
