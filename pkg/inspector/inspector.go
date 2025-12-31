package inspector

import (
	"encoding/json"
	"fmt"

	"k8s.io/utils/set"
)

var (
	noNodeAllocated = NewScheduleError(fmt.Errorf("no node allocated"))

	someTaskNotAllocated = NewScheduleError(fmt.Errorf("some tasks are not allocated"))
)

type ScheduleError struct {
	Err error
}

func (e *ScheduleError) Error() string {
	if e == nil || e.Err == nil {
		return "unknown error"
	}

	return e.Err.Error()
}

func (e *ScheduleError) MarshalJSON() ([]byte, error) {
	return json.Marshal(e.Error())
}

func NewScheduleError(err error) *ScheduleError {
	return &ScheduleError{
		Err: err,
	}
}

type TaskResult struct {
	Error    *ScheduleError `json:"error"`
	BadNodes map[string]any `json:"bad_nodes"`
	Node     string         `json:"node"`
}

func (tr *TaskResult) initError() {
	if tr.Error != nil {
		return
	}
	if tr.Node == "" {
		tr.Error = noNodeAllocated
	}
}
func (tr *TaskResult) AllocateNode() (string, *ScheduleError) {
	tr.initError()
	return tr.Node, tr.Error
}

type ScheduleResult struct {
	Error *ScheduleError `json:"error"`

	Tasks map[string]*TaskResult `json:"tasks"`
}

func FailResult(e error) *ScheduleResult {
	switch err := e.(type) {
	case *ScheduleError:
		return &ScheduleResult{
			Error: err,
		}

	}
	return &ScheduleResult{
		Error: NewScheduleError(e),
	}
}

func (sr *ScheduleResult) InitError() {
	if sr.Error != nil {
		return
	}
	someError := false
	for _, tr := range sr.Tasks {
		tr.initError()
		if tr.Error != nil {
			someError = true
		}
	}
	if someError {
		sr.Error = someTaskNotAllocated
	}
}

func (sr *ScheduleResult) AllocateNodes() (set.Set[string], *ScheduleError) {
	if sr == nil {
		return nil, noNodeAllocated
	}
	if sr.Error != nil {
		return nil, sr.Error
	}
	res := set.New[string]()
	for _, tr := range sr.Tasks {
		res.Insert(tr.Node)
	}
	return res, nil
}

func (sr *ScheduleResult) OneTaskResult() (tr *TaskResult) {
	if sr == nil {
		return &TaskResult{
			Error: noNodeAllocated,
		}
	}
	if sr.Error != nil {
		return &TaskResult{
			Error: sr.Error,
		}
	}
	for _, tr := range sr.Tasks {
		tr.initError()
		return tr
	}
	return &TaskResult{
		Error: noNodeAllocated,
	}
}

func (sr *ScheduleResult) String() string {
	if sr == nil {
		return ""
	}
	bx, e := json.Marshal(sr)
	if e != nil {
		return e.Error()
	}
	return string(bx)
}
