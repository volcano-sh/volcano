package validate

import (
	"fmt"

	yaml "gopkg.in/yaml.v2"

	"k8s.io/api/admission/v1beta1"
	whv1beta1 "k8s.io/api/admissionregistration/v1beta1"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog"
	"strings"
	"volcano.sh/volcano/pkg/webhooks/schema"
	"volcano.sh/volcano/pkg/webhooks/util"

	"volcano.sh/volcano/pkg/scheduler/conf"
	"volcano.sh/volcano/pkg/webhooks/router"
)

const (
	configmapName          = "volcano-scheduler-configmap"
	enqueueName            = "enqueue"
	overCommitFactor       = "overcommit-factor"
	overCommitFactorMinVal = 1.0
)

func init() {
	router.RegisterAdmission(service)
}

var service = &router.AdmissionService{
	Path: "/configmaps/validate",
	Func: AdmitConfigmaps,

	Config: config,

	ValidatingConfig: &whv1beta1.ValidatingWebhookConfiguration{
		Webhooks: []whv1beta1.ValidatingWebhook{{
			Name: "validateconfigmap.volcano.sh",
			Rules: []whv1beta1.RuleWithOperations{
				{
					Operations: []whv1beta1.OperationType{whv1beta1.Create, whv1beta1.Update},
					Rule: whv1beta1.Rule{
						APIGroups:   []string{""},
						APIVersions: []string{"v1"},
						Resources:   []string{"configmaps"},
					},
				},
			},
		}},
	},
}

var config = &router.AdmissionServiceConfig{}

// AdmitConfigmaps is to admit configMap for volcano scheduler and return response.
func AdmitConfigmaps(ar v1beta1.AdmissionReview) *v1beta1.AdmissionResponse {
	klog.V(3).Infof("admitting configmap -- %s", ar.Request.Operation)

	cm, err := schema.DecodeConfigmap(ar.Request.Object, ar.Request.Resource)
	if err != nil {
		return util.ToAdmissionResponse(err)
	}

	var msg string
	reviewResponse := v1beta1.AdmissionResponse{}
	reviewResponse.Allowed = true

	switch ar.Request.Operation {
	case v1beta1.Create, v1beta1.Update:
		msg = validateConfigmap(cm, &reviewResponse)
	default:
		err := fmt.Errorf("expect operation to be 'CREATE' or 'UPDATE'")
		return util.ToAdmissionResponse(err)
	}

	if !reviewResponse.Allowed {
		reviewResponse.Result = &metav1.Status{Message: strings.TrimSpace(msg)}
	}
	return &reviewResponse
}

/*
allow config to create and update when
1. configmap name must be "volcano-scheduler-configmap"
2. config data must be only one
3. enqueue argument "overcommit-factor" must be greater than 1 if existed
*/
func validateConfigmap(cm *v1.ConfigMap, reviewResponse *v1beta1.AdmissionResponse) string {
	msg := ""
	if cm.Name != configmapName {
		return msg
	}

	confKey := ""
	confNum := 0
	for key := range cm.Data {
		if strings.HasSuffix(key, ".conf") {
			confNum++
			confKey = key
		}
	}

	if confNum != 1 {
		reviewResponse.Allowed = false
		return fmt.Errorf("%s data format is err, config file = %d ", configmapName, confNum).Error()
	}

	data := cm.Data[confKey]
	schedulerConf := &conf.SchedulerConfiguration{}
	buf := make([]byte, len(data))
	copy(buf, data)

	if err := yaml.Unmarshal(buf, schedulerConf); err != nil {
		reviewResponse.Allowed = false
		return fmt.Errorf("%s data %s Unmarshal failed", configmapName, confKey).Error()
	}

	err := enqueueConfCheck(schedulerConf.Configurations)
	if err != nil {
		reviewResponse.Allowed = false
		return err.Error()
	}

	return msg
}

// check enqueue action's legality
func enqueueConfCheck(configurations []conf.Configuration) error {
	actionArg := GetArgOfActionFromConf(configurations, enqueueName)
	if actionArg == nil {
		return nil
	}

	var factor float64
	actionArg.GetFloat64(&factor, overCommitFactor)
	if factor < overCommitFactorMinVal {
		return fmt.Errorf("enqueue argument %s must be more than %v", overCommitFactor, overCommitFactorMinVal)
	}

	return nil
}
