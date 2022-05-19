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
	ErrorMessage string `json:"errorMessage"`
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
