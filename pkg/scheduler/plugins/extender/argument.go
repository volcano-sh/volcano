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

package extender

import "volcano.sh/volcano/pkg/scheduler/api"

type OnSessionOpenRequest struct {
	Jobs           map[api.JobID]*api.JobInfo
	Nodes          map[string]*api.NodeInfo
	Queues         map[api.QueueID]*api.QueueInfo
	NamespaceInfo  map[api.NamespaceName]*api.NamespaceInfo
	RevocableNodes map[string]*api.NodeInfo
	NodeList       []string
}

type OnSessionOpenResponse struct{}

type OnSessionCloseRequest struct{}
type OnSessionCloseResponse struct{}

type PredicateRequest struct {
	Task *api.TaskInfo `json:"task"`
	Node *api.NodeInfo `json:"node"`
}

type PredicateResponse struct {
	ErrorMessage string `json:"status"`
	Code         int    `json:"code"`
}

type PrioritizeRequest struct {
	Task  *api.TaskInfo   `json:"task"`
	Nodes []*api.NodeInfo `json:"nodes"`
}

type PrioritizeResponse struct {
	NodeScore    map[string]float64 `json:"nodeScore"`
	ErrorMessage string             `json:"errorMessage"`
}

type PreemptableRequest struct {
	Evictor  *api.TaskInfo   `json:"evictor"`
	Evictees []*api.TaskInfo `json:"evictees"`
}

type PreemptableResponse struct {
	Status  int             `json:"status"`
	Victims []*api.TaskInfo `json:"victims"`
}

type ReclaimableRequest PreemptableRequest
type ReclaimableResponse PreemptableResponse

type JobEnqueueableRequest struct {
	Job *api.JobInfo `json:"job"`
}

type JobEnqueueableResponse struct {
	Status int `json:"status"`
}

type QueueOverusedRequest struct {
	Queue *api.QueueInfo `json:"queue"`
}
type QueueOverusedResponse struct {
	Overused bool `json:"overused"`
}

type JobReadyRequest struct {
	Job *api.JobInfo `json:"job"`
}

type JobReadyResponse struct {
	Status bool `json:"status"`
}

type EventHandlerRequest struct {
	Task *api.TaskInfo `json:"task"`
}

type EventHandlerResponse struct {
	ErrorMessage string `json:"errorMessage"`
}
