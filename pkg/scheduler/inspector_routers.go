package scheduler

import (
	"net/http"

	"github.com/creasty/defaults"
	"github.com/gin-gonic/gin"
	"github.com/go-playground/validator/v10"
	"github.com/google/uuid"
	coreV1 "k8s.io/api/core/v1"
	"k8s.io/klog/v2"
	"volcano.sh/volcano/pkg/inspector"
)

// Router godoc
// @title schedule_inspector
// @version 1.0`
// @description schedule_inspector API
// @termsOfService [schedule_inspector](https://github.com/volcano-sh/volcano/pull/4740)
// @tag.name schedule_inspector
// @tag.docs.url https://github.com/volcano-sh/volcano/pull/4740
// @tag.docs.description schedule_inspector description
// @license.name Apache 2.0
// @BasePath /api/v1/scheduling
func Router(si *ScheduleInspector) *gin.Engine {
	r := gin.New()
	r.Use(gin.Recovery())

	r.NoRoute(func(c *gin.Context) {
		c.String(http.StatusNotFound, `API:%s not found`, c.Request.URL.Path)
	})

	v1 := r.Group("/api/v1")
	{
		scheduling := v1.Group("/scheduling")
		{
			scheduling.POST(`/dryrun`, si.DryRun)
			scheduling.GET(`/reason/:namespace/:workload/:name`, si.Reason)
		}
	}

	return r
}

type TaskTemplate struct {
	Replicas int32          `json:"replicas" validate:"required,min=1"`
	Spec     coreV1.PodSpec `json:"spec" validate:"required"`
}
type DryrunRequest struct {
	UUID string `json:"uuid"`

	Namespace string         `json:"namespace" default:"default"`
	Queue     string         `json:"queue" default:"default"`
	Tasks     []TaskTemplate `json:"tasks" validate:"required"`
}

func (d *DryrunRequest) SetDefaults() {
	if defaults.CanUpdate(d.UUID) {
		d.UUID = uuid.NewString()
	}
}

type ReasonRequest struct {
	UUID      string `json:"uuid"`
	Namespace string `json:"namespace" uri:"namespace" validate:"required"`
	Workload  string `json:"workload" uri:"workload" validate:"required,oneof=vcjob statefulset deploy job pod"`
	Name      string `json:"name" uri:"name" validate:"required"`
}

func (d *ReasonRequest) SetDefaults() {
	if defaults.CanUpdate(d.UUID) {
		d.UUID = uuid.NewString()
	}
}

type BaseResponse struct {
	UUID    string `json:"uuid"`
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data"`
}

type ScheduleResponse struct {
	BaseResponse
	Data *inspector.TaskResult `json:"data"`
}

func Success(uuid string, data any) *BaseResponse {
	return &BaseResponse{
		UUID:    uuid,
		Code:    0,
		Message: "success",
		Data:    data,
	}
}

func Failed(uuid string, e error) *BaseResponse {
	return &BaseResponse{
		UUID:    uuid,
		Code:    1,
		Message: e.Error(),
	}
}

func setStructDefault[T any](p T) {
	e := defaults.Set(p)
	if e != nil {
		klog.V(1).Info(e)
	}
}

// use a single instance of Validate, it caches struct info
var validate = validator.New()

func Validate[T any](req T) error {
	setStructDefault(req)
	return validate.Struct(req)
}

// DryRun godoc
// @Summary DryRun
// @Description dryrun schedule once
// @Accept json
// @Produce json
// @Param body body DryrunRequest true "request body"
// @Success 200 {object} ScheduleResponse
// @Router /api/v1/scheduling/dryrun [POST]
func (si *ScheduleInspector) DryRun(ctx *gin.Context) {
	req := &DryrunRequest{}

	err := ctx.BindJSON(req)
	if err != nil {
		ctx.JSON(http.StatusBadRequest, Failed(req.UUID, err))
		return
	}
	err = Validate(req)
	if err != nil {
		ctx.JSON(http.StatusBadRequest, Failed(req.UUID, err))
		return
	}

	ctx.JSON(http.StatusOK, Success(req.UUID, si.simulate(req)))
}

// Reason godoc
// @Summary Reason
// @Description get current workload pending reasons
// @Accept -
// @Produce json
// @Success 200 {object} ScheduleResponse
// @Router /api/v1/scheduling/reason/{namespace}/{workload}/{name} [GET]
func (si *ScheduleInspector) Reason(ctx *gin.Context) {
	req := &ReasonRequest{}

	err := ctx.BindUri(req)
	if err != nil {
		ctx.JSON(http.StatusBadRequest, Failed(req.UUID, err))
		return
	}
	err = Validate(req)
	if err != nil {
		ctx.JSON(http.StatusBadRequest, Failed(req.UUID, err))
		return
	}
	ctx.JSON(http.StatusOK, Success(req.UUID, si.reason(ctx, req)))
}
