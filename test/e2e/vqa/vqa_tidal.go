package vqa

import (
	"context"
	"github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"reflect"
	"volcano.sh/apis/pkg/apis/autoscaling/v1alpha1"
	e2eutil "volcano.sh/volcano/test/e2e/util"
)

var _ = ginkgo.Describe("Vertical Queue Autoscaling E2E Test", func() {
	ginkgo.It("One tidal schedule", func() {
		slotGuarantee := corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse("50m"),
			corev1.ResourceMemory: resource.MustParse("100Mi")}
		slotCapacity := corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse("100m"),
			corev1.ResourceMemory: resource.MustParse("200Mi")}
		ctx := e2eutil.InitTestContext(e2eutil.Options{
			NodesNumLimit:      1,
			NodesResourceLimit: slotCapacity,
		})
		queue1 := &e2eutil.QueueSpec{
			Name:              "vqa-queue1",
			Weight:            1,
			GuaranteeResource: slotGuarantee,
			Capacity:          slotCapacity,
		}
		e2eutil.CreateQueueWithQueueSpec(ctx, queue1)

		vqa := &e2eutil.VQASpec{
			Name:      "vqa1",
			QueueName: queue1.Name,
			Type:      v1alpha1.VerticalQueueAutoscalerTidalType,
			TidalSpec: &v1alpha1.TidalSpec{
				Tidal: []v1alpha1.Tidal{
					{
						Schedule: "*/2 * * * * *",
						Weight:   1,
						Capability: corev1.ResourceList{
							corev1.ResourceCPU:    resource.MustParse("10m"),
							corev1.ResourceMemory: resource.MustParse("20Mi")},
						Guarantee: corev1.ResourceList{
							corev1.ResourceCPU:    resource.MustParse("5m"),
							corev1.ResourceMemory: resource.MustParse("10Mi")},
					},
				},
			},
		}
		defer func() {
			ctx.Vcclient.SchedulingV1beta1().Queues().Delete(context.TODO(), queue1.Name, metav1.DeleteOptions{})
			ctx.Vcclient.AutoscalingV1alpha1().VerticalQueueAutoscalers().Delete(context.TODO(), vqa.Name, metav1.DeleteOptions{})
		}()
		e2eutil.CreateVQA(ctx, vqa)
		err := e2eutil.WaitQueueCapacity(func() (bool, error) {
			queue, err := ctx.Vcclient.SchedulingV1beta1().Queues().Get(context.TODO(), queue1.Name, metav1.GetOptions{})
			Expect(err).NotTo(HaveOccurred(), "Get queue %s failed", queue1.Name)
			return queue.Spec.Weight == vqa.TidalSpec.Tidal[0].Weight && reflect.DeepEqual(queue.Spec.Capability, vqa.TidalSpec.Tidal[0].Capability) &&
				reflect.DeepEqual(queue.Spec.Guarantee.Resource, vqa.TidalSpec.Tidal[0].Guarantee), nil
		})
		Expect(err).NotTo(HaveOccurred(), "Capacity of queue %s not auto scale", vqa.Name)
	})
	ginkgo.It("Two tidal schedule", func() {
		slotGuarantee := corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse("50m"),
			corev1.ResourceMemory: resource.MustParse("100Mi")}
		slotCapacity := corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse("100m"),
			corev1.ResourceMemory: resource.MustParse("200Mi")}
		ctx := e2eutil.InitTestContext(e2eutil.Options{
			NodesNumLimit:      1,
			NodesResourceLimit: slotCapacity,
		})
		queue2 := &e2eutil.QueueSpec{
			Name:              "vqa-queue2",
			Weight:            1,
			GuaranteeResource: slotGuarantee,
			Capacity:          slotCapacity,
		}
		e2eutil.CreateQueueWithQueueSpec(ctx, queue2)

		vqa := &e2eutil.VQASpec{
			Name:      "vqa2",
			QueueName: queue2.Name,
			Type:      v1alpha1.VerticalQueueAutoscalerTidalType,
			TidalSpec: &v1alpha1.TidalSpec{
				Tidal: []v1alpha1.Tidal{
					{
						Schedule: "*/3 * * * * *",
						Weight:   1,
						Capability: corev1.ResourceList{
							corev1.ResourceCPU:    resource.MustParse("10m"),
							corev1.ResourceMemory: resource.MustParse("20Mi")},
						Guarantee: corev1.ResourceList{
							corev1.ResourceCPU:    resource.MustParse("5m"),
							corev1.ResourceMemory: resource.MustParse("10Mi")},
					},
					{
						Schedule: "*/5 * * * * *",
						Weight:   5,
						Capability: corev1.ResourceList{
							corev1.ResourceCPU:    resource.MustParse("50m"),
							corev1.ResourceMemory: resource.MustParse("100Mi")},
						Guarantee: corev1.ResourceList{
							corev1.ResourceCPU:    resource.MustParse("25m"),
							corev1.ResourceMemory: resource.MustParse("50Mi")},
					},
				},
			},
		}
		defer func() {
			ctx.Vcclient.SchedulingV1beta1().Queues().Delete(context.TODO(), queue2.Name, metav1.DeleteOptions{})
			ctx.Vcclient.AutoscalingV1alpha1().VerticalQueueAutoscalers().Delete(context.TODO(), vqa.Name, metav1.DeleteOptions{})
		}()
		e2eutil.CreateVQA(ctx, vqa)
		err := e2eutil.WaitQueueCapacity(func() (bool, error) {
			queue, err := ctx.Vcclient.SchedulingV1beta1().Queues().Get(context.TODO(), queue2.Name, metav1.GetOptions{})
			Expect(err).NotTo(HaveOccurred(), "Get queue %s failed", queue2.Name)
			return queue.Spec.Weight == vqa.TidalSpec.Tidal[0].Weight && reflect.DeepEqual(queue.Spec.Capability, vqa.TidalSpec.Tidal[0].Capability) &&
				reflect.DeepEqual(queue.Spec.Guarantee.Resource, vqa.TidalSpec.Tidal[0].Guarantee), nil
		})
		Expect(err).NotTo(HaveOccurred(), "Capacity of queue %s not auto scale down", vqa.Name)
		err = e2eutil.WaitQueueCapacity(func() (bool, error) {
			queue, err := ctx.Vcclient.SchedulingV1beta1().Queues().Get(context.TODO(), queue2.Name, metav1.GetOptions{})
			Expect(err).NotTo(HaveOccurred(), "Get queue %s failed", queue2.Name)
			return queue.Spec.Weight == vqa.TidalSpec.Tidal[1].Weight && reflect.DeepEqual(queue.Spec.Capability, vqa.TidalSpec.Tidal[1].Capability) &&
				reflect.DeepEqual(queue.Spec.Guarantee.Resource, vqa.TidalSpec.Tidal[1].Guarantee), nil
		})
		Expect(err).NotTo(HaveOccurred(), "Capacity of queue %s not auto scale up", vqa.Name)
	})
	ginkgo.It("Two VQA", func() {
		slotGuarantee := corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse("50m"),
			corev1.ResourceMemory: resource.MustParse("100Mi")}
		slotCapacity := corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse("100m"),
			corev1.ResourceMemory: resource.MustParse("200Mi")}
		ctx := e2eutil.InitTestContext(e2eutil.Options{
			NodesNumLimit:      1,
			NodesResourceLimit: slotCapacity,
		})
		queue3 := &e2eutil.QueueSpec{
			Name:              "vqa-queue3",
			Weight:            1,
			GuaranteeResource: slotGuarantee,
			Capacity:          slotCapacity,
		}
		e2eutil.CreateQueueWithQueueSpec(ctx, queue3)

		vqa3_1 := &e2eutil.VQASpec{
			Name:      "vqa3-1",
			QueueName: queue3.Name,
			Type:      v1alpha1.VerticalQueueAutoscalerTidalType,
			TidalSpec: &v1alpha1.TidalSpec{
				Tidal: []v1alpha1.Tidal{
					{
						Schedule: "*/3 * * * * *",
						Weight:   1,
						Capability: corev1.ResourceList{
							corev1.ResourceCPU:    resource.MustParse("10m"),
							corev1.ResourceMemory: resource.MustParse("20Mi")},
						Guarantee: corev1.ResourceList{
							corev1.ResourceCPU:    resource.MustParse("5m"),
							corev1.ResourceMemory: resource.MustParse("10Mi")},
					},
				},
			},
		}
		vqa3_2 := &e2eutil.VQASpec{
			Name:      "vqa3-2",
			QueueName: queue3.Name,
			Type:      v1alpha1.VerticalQueueAutoscalerTidalType,
			TidalSpec: &v1alpha1.TidalSpec{
				Tidal: []v1alpha1.Tidal{
					{
						Schedule: "*/5 * * * * *",
						Weight:   5,
						Capability: corev1.ResourceList{
							corev1.ResourceCPU:    resource.MustParse("50m"),
							corev1.ResourceMemory: resource.MustParse("100Mi")},
						Guarantee: corev1.ResourceList{
							corev1.ResourceCPU:    resource.MustParse("25m"),
							corev1.ResourceMemory: resource.MustParse("50Mi")},
					},
				},
			},
		}
		defer func() {
			ctx.Vcclient.SchedulingV1beta1().Queues().Delete(context.TODO(), queue3.Name, metav1.DeleteOptions{})
			ctx.Vcclient.AutoscalingV1alpha1().VerticalQueueAutoscalers().Delete(context.TODO(), vqa3_1.Name, metav1.DeleteOptions{})
			ctx.Vcclient.AutoscalingV1alpha1().VerticalQueueAutoscalers().Delete(context.TODO(), vqa3_2.Name, metav1.DeleteOptions{})
		}()
		e2eutil.CreateVQA(ctx, vqa3_1)
		e2eutil.CreateVQA(ctx, vqa3_2)
		err := e2eutil.WaitQueueCapacity(func() (bool, error) {
			queue, err := ctx.Vcclient.SchedulingV1beta1().Queues().Get(context.TODO(), queue3.Name, metav1.GetOptions{})
			Expect(err).NotTo(HaveOccurred(), "Get queue %s failed", queue3.Name)
			return queue.Spec.Weight == vqa3_1.TidalSpec.Tidal[0].Weight && reflect.DeepEqual(queue.Spec.Capability, vqa3_1.TidalSpec.Tidal[0].Capability) &&
				reflect.DeepEqual(queue.Spec.Guarantee.Resource, vqa3_1.TidalSpec.Tidal[0].Guarantee), nil
		})
		Expect(err).NotTo(HaveOccurred(), "Capacity of queue %s not auto scale down", vqa3_1.Name)
		err = e2eutil.WaitQueueCapacity(func() (bool, error) {
			queue, err := ctx.Vcclient.SchedulingV1beta1().Queues().Get(context.TODO(), queue3.Name, metav1.GetOptions{})
			Expect(err).NotTo(HaveOccurred(), "Get queue %s failed", queue3.Name)
			return queue.Spec.Weight == vqa3_2.TidalSpec.Tidal[0].Weight && reflect.DeepEqual(queue.Spec.Capability, vqa3_2.TidalSpec.Tidal[0].Capability) &&
				reflect.DeepEqual(queue.Spec.Guarantee.Resource, vqa3_2.TidalSpec.Tidal[0].Guarantee), nil
		})
		Expect(err).NotTo(HaveOccurred(), "Capacity of queue %s not auto scale up", vqa3_2.Name)
	})
})
