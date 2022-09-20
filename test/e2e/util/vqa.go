package util

import (
	"context"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"time"
	"volcano.sh/apis/pkg/apis/autoscaling/v1alpha1"
)

type VQASpec struct {
	Name       string
	QueueName  string
	Type       v1alpha1.VerticalQueueAutoscalerType
	TidalSpec  *v1alpha1.TidalSpec
	MetricSpec []v1alpha1.MetricSpec
}

func CreateVQA(ctx *TestContext, vqaSpec *VQASpec) {
	_, err := ctx.Vcclient.AutoscalingV1alpha1().VerticalQueueAutoscalers().Get(context.TODO(), vqaSpec.Name, metav1.GetOptions{})
	if err != nil {
		vqa := &v1alpha1.VerticalQueueAutoscaler{
			ObjectMeta: metav1.ObjectMeta{
				Name: vqaSpec.Name,
			},
			Spec: v1alpha1.VQASpec{
				Queue: vqaSpec.QueueName,
				Type:  vqaSpec.Type,
			},
		}
		if vqaSpec.TidalSpec != nil {
			vqa.Spec.TidalSpec = *vqaSpec.TidalSpec
		}
		if len(vqaSpec.MetricSpec) != 0 {
			vqa.Spec.MetricSpec = vqaSpec.MetricSpec
		}
		_, err := ctx.Vcclient.AutoscalingV1alpha1().VerticalQueueAutoscalers().Create(context.TODO(), vqa, metav1.CreateOptions{})
		Expect(err).NotTo(HaveOccurred(), "failed to create vqa %s", vqa.Name)
	}
}

func WaitQueueCapacity(condition func() (bool, error)) error {
	return wait.Poll(100*time.Millisecond, FiveMinute, condition)
}
