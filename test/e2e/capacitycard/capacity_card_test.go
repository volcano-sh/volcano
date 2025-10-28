/*
Copyright 2025 The Volcano Authors.

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

package capacitycard

import (
	"context"
	"fmt"
	"math/rand"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	batchv1alpha1 "volcano.sh/apis/pkg/apis/batch/v1alpha1"
	schedulingv1beta1 "volcano.sh/apis/pkg/apis/scheduling/v1beta1"
	e2eutil "volcano.sh/volcano/test/e2e/util"
)

// 生成随机后缀，确保资源名称唯一性
func generateRandomSuffix() string {
	r := rand.New(rand.NewSource(time.Now().UnixNano()))
	return fmt.Sprintf("%d", r.Intn(10000))
}

// 全局常量定义
const (
	// 集群中实际的卡片类型
	CardTypeTeslaK80 = "Tesla-K80"
	CardTypeRTX4090  = "NVIDIA-GeForce-RTX-4090"
	CardTypeH800     = "NVIDIA-H800"
	// 轮询配置
	PollInterval       = 500 * time.Millisecond
	QueueReadyTimeout  = 30 * time.Second
	JobProcessTimeout  = 60 * time.Second
	CleanupGracePeriod = 10 * time.Second
)

// 初始化随机数生成器
var _ = BeforeSuite(func() {
	rand.Seed(time.Now().UnixNano())
})

var _ = Describe("Capacity Card E2E Test", func() {
	Context("Capacity Card - Basic", func() {
		// 测试1: 基本队列容量管理测试
		It("Queue Capacity Management", func() {
			randomSuffix := generateRandomSuffix()
			fmt.Printf("测试1: 生成随机后缀 %s\n", randomSuffix)
			ctx := e2eutil.InitTestContext(e2eutil.Options{
				Namespace: fmt.Sprintf("capacity-card-test-1-%s", randomSuffix),
			})
			fmt.Printf("测试1: 测试上下文初始化完成，命名空间 %s\n", ctx.Namespace)
			defer e2eutil.CleanupTestContext(ctx)

			// 创建带卡片配额的队列
			queueSpec := &e2eutil.QueueSpec{
				Name:   fmt.Sprintf("capacity-test-queue-%s", randomSuffix),
				Weight: 10,
				Capacity: v1.ResourceList{
					v1.ResourceCPU:    resource.MustParse("2"),
					v1.ResourceMemory: resource.MustParse("2Gi"),
				},
				Annotations: map[string]string{
					"volcano.sh/card.quota": fmt.Sprintf(`{"%s": 4}`, CardTypeTeslaK80),
				},
			}

			// 使用e2eutil函数创建队列
			fmt.Printf("测试1: 开始创建队列 %s\n", queueSpec.Name)
			e2eutil.CreateQueueWithQueueSpec(ctx, queueSpec)
			fmt.Printf("测试1: 队列 %s 创建成功，%s卡片配额为4\n", queueSpec.Name, CardTypeTeslaK80)

			// 等待队列状态变为开放
			fmt.Printf("测试1: 等待队列 %s 状态变为开放\n", queueSpec.Name)
			queueOpenErr := e2eutil.WaitQueueStatus(func() (bool, error) {
				queue, err := ctx.Vcclient.SchedulingV1beta1().Queues().Get(context.TODO(), queueSpec.Name, metav1.GetOptions{})
				if err != nil {
					return false, err
				}
				return queue.Status.State == schedulingv1beta1.QueueStateOpen, nil
			})
			Expect(queueOpenErr).NotTo(HaveOccurred(), "队列未能在超时时间内变为开放状态")
			fmt.Printf("测试1: 队列 %s 状态已变为开放\n", queueSpec.Name)

			// 使用e2eutil清理队列
			defer func() {
				e2eutil.DeleteQueue(ctx, queueSpec.Name)
				fmt.Printf("测试1: 队列 %s 已清理\n", queueSpec.Name)
			}()
		})
	})

	Context("Capacity Card - VCJob", func() {
		// 测试2: 作业入队性检查 - 成功案例
		It("Job Enqueueable Check - Success", func() {
			randomSuffix := generateRandomSuffix()
			fmt.Printf("测试2: 生成随机后缀 %s\n", randomSuffix)
			ctx := e2eutil.InitTestContext(e2eutil.Options{
				Namespace: fmt.Sprintf("capacity-card-test-2-%s", randomSuffix),
			})
			fmt.Printf("测试2: 测试上下文初始化完成，命名空间 %s\n", ctx.Namespace)
			defer e2eutil.CleanupTestContext(ctx)

			// 创建带卡片配额的队列
			queueSpec := &e2eutil.QueueSpec{
				Name:   fmt.Sprintf("enqueue-test-queue-%s", randomSuffix),
				Weight: 10,
				Capacity: v1.ResourceList{
					v1.ResourceCPU:    resource.MustParse("8"),
					v1.ResourceMemory: resource.MustParse("8Gi"),
				},
				Annotations: map[string]string{
					"volcano.sh/card.quota": fmt.Sprintf(`{"%s": 4}`, CardTypeTeslaK80),
				},
			}

			// 使用e2eutil函数创建队列
			fmt.Printf("测试2: 开始创建队列 %s\n", queueSpec.Name)
			e2eutil.CreateQueueWithQueueSpec(ctx, queueSpec)
			fmt.Printf("测试2: 队列 %s 创建成功\n", queueSpec.Name)

			// 等待队列状态变为开放
			fmt.Printf("测试2: 等待队列 %s 状态变为开放\n", queueSpec.Name)
			queueOpenErr := e2eutil.WaitQueueStatus(func() (bool, error) {
				queue, err := ctx.Vcclient.SchedulingV1beta1().Queues().Get(context.TODO(), queueSpec.Name, metav1.GetOptions{})
				if err != nil {
					return false, err
				}
				return queue.Status.State == schedulingv1beta1.QueueStateOpen, nil
			})
			Expect(queueOpenErr).NotTo(HaveOccurred(), "队列未能在超时时间内变为开放状态")
			fmt.Printf("测试2: 队列 %s 状态已变为开放\n", queueSpec.Name)

			// 创建一个带卡片请求的作业
			jobSpec := &e2eutil.JobSpec{
				Name:  fmt.Sprintf("enqueue-success-job-%s", randomSuffix),
				Queue: queueSpec.Name,
				Tasks: []e2eutil.TaskSpec{
					{
						Name: "task",
						Min:  1,
						Rep:  2,
						Img:  e2eutil.DefaultNginxImage,
						Req: v1.ResourceList{
							v1.ResourceCPU:                    resource.MustParse("1"),
							v1.ResourceMemory:                 resource.MustParse("1Gi"),
							v1.ResourceName("nvidia.com/gpu"): resource.MustParse("2"),
						},
						Limit: v1.ResourceList{
							v1.ResourceCPU:                    resource.MustParse("1"),
							v1.ResourceMemory:                 resource.MustParse("1Gi"),
							v1.ResourceName("nvidia.com/gpu"): resource.MustParse("2"),
						},
					},
				},
			}

			// 添加作业卡片请求注解到JobSpec
			jobSpec.Annotations = map[string]string{
				"volcano.sh/card.request": fmt.Sprintf(`{"%s": 2}`, CardTypeTeslaK80),
			}

			// 直接创建作业
			fmt.Printf("测试2: 开始创建作业 %s\n", jobSpec.Name)
			job := e2eutil.CreateJob(ctx, jobSpec)
			fmt.Printf("测试2: 作业 %s 创建成功\n", job.Name)

			// 等待作业就绪
			fmt.Printf("测试2: 等待作业就绪\n")
			err := e2eutil.WaitJobReady(ctx, job)
			Expect(err).NotTo(HaveOccurred(), "作业未能在超时时间内就绪")
			fmt.Printf("测试2: 作业 %s 已就绪\n", job.Name)

			// 清理资源
			defer func() {
				// 删除作业
				e2eutil.DeleteJob(ctx, job)
				fmt.Printf("测试2: 作业 %s 已清理\n", job.Name)

				// 使用e2eutil清理队列
				e2eutil.DeleteQueue(ctx, queueSpec.Name)
				fmt.Printf("测试2: 队列 %s 已清理\n", queueSpec.Name)
			}()
		})

		// 测试3: 任务卡片资源分配测试
		It("Task Card Resource Allocation", func() {
			randomSuffix := generateRandomSuffix()
			fmt.Printf("测试3: 生成随机后缀 %s\n", randomSuffix)
			ctx := e2eutil.InitTestContext(e2eutil.Options{
				Namespace: fmt.Sprintf("capacity-card-test-3-%s", randomSuffix),
			})
			fmt.Printf("测试3: 测试上下文初始化完成，命名空间 %s 创建成功\n", ctx.Namespace)
			defer e2eutil.CleanupTestContext(ctx)

			// 创建带卡片配额的队列
			queueSpec := &e2eutil.QueueSpec{
				Name:   fmt.Sprintf("allocation-test-queue-%s", randomSuffix),
				Weight: 10,
				Capacity: v1.ResourceList{
					v1.ResourceCPU:    resource.MustParse("2"),
					v1.ResourceMemory: resource.MustParse("2Gi"),
				},
				Annotations: map[string]string{
					"volcano.sh/card.quota": fmt.Sprintf(`{"%s": 4}`, CardTypeTeslaK80),
				},
			}

			// 使用e2eutil函数创建队列
			fmt.Printf("测试3: 开始创建队列 %s\n", queueSpec.Name)
			e2eutil.CreateQueueWithQueueSpec(ctx, queueSpec)
			fmt.Printf("测试3: 队列 %s 创建成功\n", queueSpec.Name)

			// 等待队列状态变为开放
			fmt.Printf("测试3: 等待队列 %s 状态变为开放\n", queueSpec.Name)
			queueOpenErr := e2eutil.WaitQueueStatus(func() (bool, error) {
				queue, err := ctx.Vcclient.SchedulingV1beta1().Queues().Get(context.TODO(), queueSpec.Name, metav1.GetOptions{})
				if err != nil {
					return false, err
				}
				return queue.Status.State == schedulingv1beta1.QueueStateOpen, nil
			})
			Expect(queueOpenErr).NotTo(HaveOccurred(), "队列未能在超时时间内变为开放状态")
			fmt.Printf("测试3: 队列 %s 状态已变为开放\n", queueSpec.Name)

			// 创建一个带任务级卡片名称请求的作业，将卡片名称注解设置到TaskSpec
			taskSpecs := []e2eutil.TaskSpec{
				{
					Name: "card-task",
					Min:  1,
					Rep:  1,
					Img:  e2eutil.DefaultNginxImage,
					Req: v1.ResourceList{
						v1.ResourceCPU:                    resource.MustParse("1"),
						v1.ResourceMemory:                 resource.MustParse("1Gi"),
						v1.ResourceName("nvidia.com/gpu"): resource.MustParse("1"),
					},
					Limit: v1.ResourceList{
						v1.ResourceCPU:                    resource.MustParse("1"),
						v1.ResourceMemory:                 resource.MustParse("1Gi"),
						v1.ResourceName("nvidia.com/gpu"): resource.MustParse("1"),
					},
					Labels: map[string]string{"card-test": "true"},
					Annotations: map[string]string{
						"volcano.sh/card.name": CardTypeTeslaK80,
					},
				},
			}

			jobSpec := &e2eutil.JobSpec{
				Name:  fmt.Sprintf("task-card-test-job-%s", randomSuffix),
				Queue: queueSpec.Name,
				Tasks: taskSpecs,
			}

			// 直接创建作业
			fmt.Printf("测试3: 开始创建作业 %s\n", jobSpec.Name)
			job := e2eutil.CreateJob(ctx, jobSpec)
			fmt.Printf("测试3: 作业 %s 创建成功\n", job.Name)

			// 等待作业就绪
			fmt.Printf("测试3: 等待作业就绪\n")
			err := e2eutil.WaitJobReady(ctx, job)
			Expect(err).NotTo(HaveOccurred(), "作业未能在超时时间内就绪")
			fmt.Printf("测试3: 作业 %s 已就绪\n", job.Name)

			// 清理资源
			defer func() {
				// 删除作业
				e2eutil.DeleteJob(ctx, job)
				fmt.Printf("测试3: 作业 %s 已清理\n", job.Name)

				// 使用e2eutil清理队列
				e2eutil.DeleteQueue(ctx, queueSpec.Name)
				fmt.Printf("测试3: 队列 %s 已清理\n", queueSpec.Name)
			}()
		})

		// 测试4: 卡片资源超出配额测试
		It("Card Resource Quota Exceeded", func() {
			randomSuffix := generateRandomSuffix()
			fmt.Printf("测试4: 生成随机后缀 %s\n", randomSuffix)
			ctx := e2eutil.InitTestContext(e2eutil.Options{
				Namespace: fmt.Sprintf("capacity-card-test-4-%s", randomSuffix),
			})
			fmt.Printf("测试4: 测试上下文初始化完成，命名空间 %s 创建成功\n", ctx.Namespace)
			defer e2eutil.CleanupTestContext(ctx)

			// 创建带有限卡片配额的队列
			queueSpec := &e2eutil.QueueSpec{
				Name:   fmt.Sprintf("quota-limit-queue-%s", randomSuffix),
				Weight: 10,
				Capacity: v1.ResourceList{
					v1.ResourceCPU:    resource.MustParse("4"),
					v1.ResourceMemory: resource.MustParse("4Gi"),
				},
				Annotations: map[string]string{
					"volcano.sh/card.quota": fmt.Sprintf(`{"%s": 2}`, CardTypeTeslaK80),
				},
			}

			// 使用e2eutil函数创建队列
			fmt.Printf("测试4: 开始创建队列 %s\n", queueSpec.Name)
			e2eutil.CreateQueueWithQueueSpec(ctx, queueSpec)
			fmt.Printf("测试4: 队列 %s 创建成功，%s卡片配额为2\n", queueSpec.Name, CardTypeTeslaK80)

			// 等待队列状态变为开放
			fmt.Printf("测试4: 等待队列 %s 状态变为开放\n", queueSpec.Name)
			queueOpenErr := e2eutil.WaitQueueStatus(func() (bool, error) {
				queue, err := ctx.Vcclient.SchedulingV1beta1().Queues().Get(context.TODO(), queueSpec.Name, metav1.GetOptions{})
				if err != nil {
					return false, err
				}
				return queue.Status.State == schedulingv1beta1.QueueStateOpen, nil
			})
			Expect(queueOpenErr).NotTo(HaveOccurred(), "队列未能在超时时间内变为开放状态")
			fmt.Printf("测试4: 队列 %s 状态已变为开放\n", queueSpec.Name)

			// 创建第一个作业，使用部分卡片配额
			job1Spec := &e2eutil.JobSpec{
				Name:  fmt.Sprintf("first-quota-job-%s", randomSuffix),
				Queue: queueSpec.Name,
				Tasks: []e2eutil.TaskSpec{
					{
						Name: "task",
						Min:  1,
						Rep:  1,
						Img:  e2eutil.DefaultNginxImage,
						Req: v1.ResourceList{
							v1.ResourceCPU:                    resource.MustParse("1"),
							v1.ResourceMemory:                 resource.MustParse("1Gi"),
							v1.ResourceName("nvidia.com/gpu"): resource.MustParse("1"),
						},
						Limit: v1.ResourceList{
							v1.ResourceCPU:                    resource.MustParse("1"),
							v1.ResourceMemory:                 resource.MustParse("1Gi"),
							v1.ResourceName("nvidia.com/gpu"): resource.MustParse("1"),
						},
						Annotations: map[string]string{
							"volcano.sh/card.name": CardTypeTeslaK80,
						},
					},
				},
				Annotations: map[string]string{
					"volcano.sh/card.request": fmt.Sprintf(`{"%s": 1}`, CardTypeTeslaK80),
				},
			}

			fmt.Printf("测试4: 开始创建第一个作业 %s\n", job1Spec.Name)
			job1 := e2eutil.CreateJob(ctx, job1Spec)
			fmt.Printf("测试4: 作业1 %s 创建成功，请求%s卡片资源为1\n", job1.Name, CardTypeTeslaK80)
			fmt.Printf("测试4: 作业1 %s 验证成功\n", job1.Name)

			// 创建第二个作业，使用剩余卡片配额
			job2Spec := &e2eutil.JobSpec{
				Name:  fmt.Sprintf("second-quota-job-%s", randomSuffix),
				Queue: queueSpec.Name,
				Tasks: []e2eutil.TaskSpec{
					{
						Name: "task",
						Min:  1,
						Rep:  1,
						Img:  e2eutil.DefaultNginxImage,
						Req: v1.ResourceList{
							v1.ResourceCPU:                    resource.MustParse("1"),
							v1.ResourceMemory:                 resource.MustParse("1Gi"),
							v1.ResourceName("nvidia.com/gpu"): resource.MustParse("1"),
						},
						Limit: v1.ResourceList{
							v1.ResourceCPU:                    resource.MustParse("1"),
							v1.ResourceMemory:                 resource.MustParse("1Gi"),
							v1.ResourceName("nvidia.com/gpu"): resource.MustParse("1"),
						},
						Annotations: map[string]string{
							"volcano.sh/card.name": CardTypeTeslaK80,
						},
					},
				},
				Annotations: map[string]string{
					"volcano.sh/card.request": fmt.Sprintf(`{"%s": 1}`, CardTypeTeslaK80),
				},
			}

			fmt.Printf("测试4: 开始创建第二个作业 %s\n", job2Spec.Name)
			job2 := e2eutil.CreateJob(ctx, job2Spec)
			fmt.Printf("测试4: 作业2 %s 创建成功，请求%s卡片资源为1\n", job2.Name, CardTypeTeslaK80)
			fmt.Printf("测试4: 作业2 %s 验证成功\n", job2.Name)

			// 创建第三个作业，尝试使用超过剩余配额的卡片资源
			job3Spec := &e2eutil.JobSpec{
				Name:  fmt.Sprintf("third-quota-job-%s", randomSuffix),
				Queue: queueSpec.Name,
				Tasks: []e2eutil.TaskSpec{
					{
						Name: "task",
						Min:  1,
						Rep:  2,
						Img:  e2eutil.DefaultNginxImage,
						Req: v1.ResourceList{
							v1.ResourceCPU:                    resource.MustParse("1"),
							v1.ResourceMemory:                 resource.MustParse("1Gi"),
							v1.ResourceName("nvidia.com/gpu"): resource.MustParse("3"),
						},
						Limit: v1.ResourceList{
							v1.ResourceCPU:                    resource.MustParse("1"),
							v1.ResourceMemory:                 resource.MustParse("1Gi"),
							v1.ResourceName("nvidia.com/gpu"): resource.MustParse("3"),
						},
						Annotations: map[string]string{
							"volcano.sh/card.name": CardTypeTeslaK80,
						},
					},
				},
				Annotations: map[string]string{
					"volcano.sh/card.request": fmt.Sprintf(`{"%s": 1}`, CardTypeTeslaK80), // 超过队列剩余配额
				},
			}

			fmt.Printf("测试4: 开始创建第三个作业 %s\n", job3Spec.Name)
			job3 := e2eutil.CreateJob(ctx, job3Spec)
			fmt.Printf("测试4: 作业3 %s 创建成功，请求%s卡片资源为3（超出剩余配额）\n", job3.Name, CardTypeTeslaK80)
			fmt.Printf("测试4: 作业3 %s 验证成功\n", job3.Name)

			// 等待作业1就绪（作业1应该可以成功运行，因为没有超出配额）
			fmt.Printf("测试4: 等待作业1就绪\n")
			err := e2eutil.WaitJobReady(ctx, job1)
			Expect(err).NotTo(HaveOccurred(), "作业1未能在超时时间内就绪")
			fmt.Printf("测试4: 作业1 %s 已就绪\n", job1.Name)

			// 等待作业1就绪（作业1应该可以成功运行，因为没有超出配额）
			fmt.Printf("测试4: 等待作业2就绪\n")
			err = e2eutil.WaitJobReady(ctx, job2)
			Expect(err).NotTo(HaveOccurred(), "作业2未能在超时时间内就绪")
			fmt.Printf("测试4: 作业2 %s 已就绪\n", job2.Name)

			// 对于作业3（超出配额），等待一段时间后检查状态
			fmt.Printf("测试4: 等待一段时间检查作业3状态（预期因配额不足无法完全就绪）\n")
			time.Sleep(JobProcessTimeout / 2)

			// 检查作业3状态
			job3Updated, err := ctx.Vcclient.BatchV1alpha1().Jobs(ctx.Namespace).Get(context.TODO(), job3.Name, metav1.GetOptions{})
			Expect(err).NotTo(HaveOccurred(), "获取作业3状态失败")
			fmt.Printf("测试4: 作业3当前状态: %s, Running: %d, Pending: %d\n",
				job3Updated.Status.State, job3Updated.Status.Running, job3Updated.Status.Pending)

			// 校验作业2状态：由于超出配额，应该没有运行中的Pod
			Expect(job3Updated.Status.Running).To(Equal(int32(0)), "作业3不应该有运行中的Pod，因为超出了队列配额")

			// 检查作业2关联的Pod状态
			pods3, err := ctx.Kubeclient.CoreV1().Pods(ctx.Namespace).List(context.TODO(), metav1.ListOptions{
				LabelSelector: fmt.Sprintf("volcano.sh/job-name=%s", job3.Name),
			})
			Expect(err).NotTo(HaveOccurred(), "获取作业3的Pod列表失败")

			// 打印Pod状态信息
			for _, pod := range pods3.Items {
				fmt.Printf("测试4: 作业3的Pod %s 状态: %s\n", pod.Name, pod.Status.Phase)
			}

			// 校验Pod状态：作业2的Pod应该处于Pending状态（无法调度）
			for _, pod := range pods3.Items {
				Expect(pod.Status.Phase).To(Equal(v1.PodPending), "作业3的Pod应该处于Pending状态，因为资源配额不足")
			}

			fmt.Printf("测试4: 作业3 %s 验证完成 - 正确因配额限制无法调度\n", job3.Name)

			// 清理资源
			defer func() {
				// 删除作业
				e2eutil.DeleteJob(ctx, job1)
				fmt.Printf("测试4: 作业1 %s 已清理\n", job1.Name)
				e2eutil.DeleteJob(ctx, job2)
				fmt.Printf("测试4: 作业2 %s 已清理\n", job2.Name)
				e2eutil.DeleteJob(ctx, job3)
				fmt.Printf("测试4: 作业3 %s 已清理\n", job3.Name)

				// 使用e2eutil删除队列
				fmt.Printf("测试4: 清理队列 %s\n", queueSpec.Name)
				e2eutil.DeleteQueue(ctx, queueSpec.Name)
			}()
		})

		// 测试5: RTX4090卡片队列容量管理测试
		It("RTX4090 Card Queue Capacity Management", func() {
			randomSuffix := generateRandomSuffix()
			fmt.Printf("测试5: 生成随机后缀 %s\n", randomSuffix)
			ctx := e2eutil.InitTestContext(e2eutil.Options{
				Namespace: fmt.Sprintf("rtx4090-card-test-%s", randomSuffix),
			})
			fmt.Printf("测试5: 测试上下文初始化完成，命名空间 %s\n", ctx.Namespace)
			defer e2eutil.CleanupTestContext(ctx)

			// 创建带RTX4090卡片配额的队列
			queueSpec := &e2eutil.QueueSpec{
				Name:   fmt.Sprintf("rtx4090-test-queue-%s", randomSuffix),
				Weight: 10,
				Capacity: v1.ResourceList{
					v1.ResourceCPU:    resource.MustParse("2"),
					v1.ResourceMemory: resource.MustParse("2Gi"),
				},
				Annotations: map[string]string{
					"volcano.sh/card.quota": fmt.Sprintf(`{"%s": 4}`, CardTypeRTX4090),
				},
			}

			// 使用e2eutil函数创建队列
			fmt.Printf("测试5: 开始创建队列 %s\n", queueSpec.Name)
			e2eutil.CreateQueueWithQueueSpec(ctx, queueSpec)
			fmt.Printf("测试5: 队列 %s 创建成功，%s卡片配额为4\n", queueSpec.Name, CardTypeRTX4090)

			// 等待队列状态变为开放
			fmt.Printf("测试5: 等待队列 %s 状态变为开放\n", queueSpec.Name)
			queueOpenErr := e2eutil.WaitQueueStatus(func() (bool, error) {
				queue, err := ctx.Vcclient.SchedulingV1beta1().Queues().Get(context.TODO(), queueSpec.Name, metav1.GetOptions{})
				if err != nil {
					return false, err
				}
				return queue.Status.State == schedulingv1beta1.QueueStateOpen, nil
			})
			Expect(queueOpenErr).NotTo(HaveOccurred(), "队列未能在超时时间内变为开放状态")
			fmt.Printf("测试5: 队列 %s 状态已变为开放\n", queueSpec.Name)

			// 清理资源
			defer func() {
				// 使用e2eutil删除队列
				fmt.Printf("测试5: 清理队列 %s\n", queueSpec.Name)
				e2eutil.DeleteQueue(ctx, queueSpec.Name)
			}()
		})

		// 测试6: H800卡片队列容量管理测试
		It("H800 Card Queue Capacity Management", func() {
			randomSuffix := generateRandomSuffix()
			fmt.Printf("测试6: 生成随机后缀 %s\n", randomSuffix)
			ctx := e2eutil.InitTestContext(e2eutil.Options{
				Namespace: fmt.Sprintf("h800-card-test-%s", randomSuffix),
			})
			fmt.Printf("测试6: 测试上下文初始化完成，命名空间 %s\n", ctx.Namespace)
			defer e2eutil.CleanupTestContext(ctx)

			// 创建带H800卡片配额的队列
			queueSpec := &e2eutil.QueueSpec{
				Name:   fmt.Sprintf("h800-test-queue-%s", randomSuffix),
				Weight: 10,
				Capacity: v1.ResourceList{
					v1.ResourceCPU:    resource.MustParse("2"),
					v1.ResourceMemory: resource.MustParse("2Gi"),
				},
				Annotations: map[string]string{
					"volcano.sh/card.quota": fmt.Sprintf(`{"%s": 4}`, CardTypeH800),
				},
			}

			// 使用e2eutil函数创建队列
			fmt.Printf("测试6: 开始创建队列 %s\n", queueSpec.Name)
			e2eutil.CreateQueueWithQueueSpec(ctx, queueSpec)
			fmt.Printf("测试6: 队列 %s 创建成功，%s卡片配额为4\n", queueSpec.Name, CardTypeH800)

			// 等待队列状态变为开放
			fmt.Printf("测试6: 等待队列 %s 状态变为开放\n", queueSpec.Name)
			queueOpenErr := e2eutil.WaitQueueStatus(func() (bool, error) {
				queue, err := ctx.Vcclient.SchedulingV1beta1().Queues().Get(context.TODO(), queueSpec.Name, metav1.GetOptions{})
				if err != nil {
					return false, err
				}
				return queue.Status.State == schedulingv1beta1.QueueStateOpen, nil
			})
			Expect(queueOpenErr).NotTo(HaveOccurred(), "队列未能在超时时间内变为开放状态")
			fmt.Printf("测试6: 队列 %s 状态已变为开放\n", queueSpec.Name)

			// 清理资源
			defer func() {
				// 使用e2eutil删除队列
				fmt.Printf("测试6: 清理队列 %s\n", queueSpec.Name)
				e2eutil.DeleteQueue(ctx, queueSpec.Name)
			}()
		})

		// 测试7: 多卡片类型混合配额测试
		It("Multiple Card Types Mixed Quota Test", func() {
			randomSuffix := generateRandomSuffix()
			fmt.Printf("测试7: 生成随机后缀 %s\n", randomSuffix)
			ctx := e2eutil.InitTestContext(e2eutil.Options{
				Namespace: fmt.Sprintf("multi-card-test-%s", randomSuffix),
			})
			fmt.Printf("测试7: 测试上下文初始化完成，命名空间 %s\n", ctx.Namespace)
			defer e2eutil.CleanupTestContext(ctx)

			// 创建带多种卡片配额的队列
			queueSpec := &e2eutil.QueueSpec{
				Name:   fmt.Sprintf("multi-card-queue-%s", randomSuffix),
				Weight: 10,
				Capacity: v1.ResourceList{
					v1.ResourceCPU:    resource.MustParse("4"),
					v1.ResourceMemory: resource.MustParse("4Gi"),
				},
				Annotations: map[string]string{
					"volcano.sh/card.quota": fmt.Sprintf(`{"%s": 2, "%s": 2, "%s": 2}`,
						CardTypeTeslaK80, CardTypeRTX4090, CardTypeH800),
				},
			}

			// 使用e2eutil函数创建队列
			fmt.Printf("测试7: 开始创建队列 %s\n", queueSpec.Name)
			e2eutil.CreateQueueWithQueueSpec(ctx, queueSpec)
			fmt.Printf("测试7: 队列 %s 创建成功，混合卡片配额配置：%s:2, %s:2, %s:2\n",
				queueSpec.Name, CardTypeTeslaK80, CardTypeRTX4090, CardTypeH800)

			// 等待队列状态变为开放
			fmt.Printf("测试7: 等待队列 %s 状态变为开放\n", queueSpec.Name)
			queueOpenErr := e2eutil.WaitQueueStatus(func() (bool, error) {
				queue, err := ctx.Vcclient.SchedulingV1beta1().Queues().Get(context.TODO(), queueSpec.Name, metav1.GetOptions{})
				if err != nil {
					return false, err
				}
				return queue.Status.State == schedulingv1beta1.QueueStateOpen, nil
			})
			Expect(queueOpenErr).NotTo(HaveOccurred(), "队列未能在超时时间内变为开放状态")
			fmt.Printf("测试7: 队列 %s 状态已变为开放\n", queueSpec.Name)

			// 清理资源
			defer func() {
				// 使用e2eutil删除队列
				fmt.Printf("测试7: 清理队列 %s\n", queueSpec.Name)
				e2eutil.DeleteQueue(ctx, queueSpec.Name)
			}()
		})

		// 测试8: 多卡片类型作业请求测试
		It("Multiple Card Types Job Request Test", func() {
			randomSuffix := generateRandomSuffix()
			fmt.Printf("测试8: 生成随机后缀 %s\n", randomSuffix)
			ctx := e2eutil.InitTestContext(e2eutil.Options{
				Namespace: fmt.Sprintf("multi-card-job-test-%s", randomSuffix),
			})
			fmt.Printf("测试8: 测试上下文初始化完成，命名空间 %s\n", ctx.Namespace)
			defer e2eutil.CleanupTestContext(ctx)

			// 创建带多种卡片配额的队列
			queueSpec := &e2eutil.QueueSpec{
				Name:   fmt.Sprintf("multi-card-job-queue-%s", randomSuffix),
				Weight: 10,
				Capacity: v1.ResourceList{
					v1.ResourceCPU:    resource.MustParse("4"),
					v1.ResourceMemory: resource.MustParse("4Gi"),
				},
				Annotations: map[string]string{
					"volcano.sh/card.quota": fmt.Sprintf(`{"%s": 8, "%s": 8, "%s": 8}`,
						CardTypeTeslaK80, CardTypeRTX4090, CardTypeH800),
				},
			}

			// 使用e2eutil函数创建队列
			fmt.Printf("测试8: 开始创建队列 %s\n", queueSpec.Name)
			e2eutil.CreateQueueWithQueueSpec(ctx, queueSpec)
			fmt.Printf("测试8: 队列 %s 创建成功，混合卡片配额配置：%s:8, %s:8, %s:8\n",
				queueSpec.Name, CardTypeTeslaK80, CardTypeRTX4090, CardTypeH800)

			// 等待队列状态变为开放
			fmt.Printf("测试8: 等待队列 %s 状态变为开放\n", queueSpec.Name)
			queueOpenErr := e2eutil.WaitQueueStatus(func() (bool, error) {
				queue, err := ctx.Vcclient.SchedulingV1beta1().Queues().Get(context.TODO(), queueSpec.Name, metav1.GetOptions{})
				if err != nil {
					return false, err
				}
				return queue.Status.State == schedulingv1beta1.QueueStateOpen, nil
			})
			Expect(queueOpenErr).NotTo(HaveOccurred(), "队列未能在超时时间内变为开放状态")
			fmt.Printf("测试8: 队列 %s 状态已变为开放\n", queueSpec.Name)

			// 创建一个请求多种卡片类型的作业
			jobSpec := &e2eutil.JobSpec{
				Name:  fmt.Sprintf("multi-card-job-%s", randomSuffix),
				Queue: queueSpec.Name,
				Tasks: []e2eutil.TaskSpec{
					{
						Name: "task",
						Min:  1,
						Rep:  1,
						Img:  e2eutil.DefaultNginxImage,
						Req: v1.ResourceList{
							v1.ResourceCPU:                    resource.MustParse("1"),
							v1.ResourceMemory:                 resource.MustParse("1Gi"),
							v1.ResourceName("nvidia.com/gpu"): resource.MustParse("1"),
						},
						Limit: v1.ResourceList{
							v1.ResourceCPU:                    resource.MustParse("1"),
							v1.ResourceMemory:                 resource.MustParse("1Gi"),
							v1.ResourceName("nvidia.com/gpu"): resource.MustParse("1"),
						},
						Annotations: map[string]string{
							"volcano.sh/card.name": CardTypeTeslaK80,
						},
					},
					{
						Name: "task",
						Min:  1,
						Rep:  1,
						Img:  e2eutil.DefaultNginxImage,
						Req: v1.ResourceList{
							v1.ResourceCPU:                    resource.MustParse("1"),
							v1.ResourceMemory:                 resource.MustParse("1Gi"),
							v1.ResourceName("nvidia.com/gpu"): resource.MustParse("1"),
						},
						Limit: v1.ResourceList{
							v1.ResourceCPU:                    resource.MustParse("1"),
							v1.ResourceMemory:                 resource.MustParse("1Gi"),
							v1.ResourceName("nvidia.com/gpu"): resource.MustParse("1"),
						},
						Annotations: map[string]string{
							"volcano.sh/card.name": CardTypeRTX4090,
						},
					},
				},
				Annotations: map[string]string{
					"volcano.sh/card.request": fmt.Sprintf(`{"%s": 1, "%s": 1}`,
						CardTypeTeslaK80, CardTypeRTX4090),
				},
			}

			// 直接创建作业（不使用PodGroup）
			fmt.Printf("测试8: 开始创建多卡片类型作业 %s\n", jobSpec.Name)
			job := e2eutil.CreateJob(ctx, jobSpec)
			fmt.Printf("测试8: 多卡片类型作业 %s 创建成功，请求: %s:1, %s:1, %s:1\n",
				job.Name, CardTypeTeslaK80, CardTypeRTX4090, CardTypeH800)

			// 等待作业就绪
			fmt.Printf("测试8: 等待作业就绪\n")
			err := e2eutil.WaitJobReady(ctx, job)
			Expect(err).NotTo(HaveOccurred(), "作业未能在超时时间内就绪")
			fmt.Printf("测试8: 作业 %s 已就绪\n", job.Name)

			// 清理资源
			defer func() {
				// 删除作业
				e2eutil.DeleteJob(ctx, job)
				fmt.Printf("测试8: 作业 %s 已清理\n", job.Name)

				// 使用e2eutil删除队列
				fmt.Printf("测试8: 清理队列 %s\n", queueSpec.Name)
				e2eutil.DeleteQueue(ctx, queueSpec.Name)
			}()
		})

		// 测试9: 基于卡片类型的优先级调度测试
		It("Card Type Based Priority Scheduling Test", func() {
			randomSuffix := generateRandomSuffix()
			fmt.Printf("测试9: 生成随机后缀 %s\n", randomSuffix)
			ctx := e2eutil.InitTestContext(e2eutil.Options{
				Namespace: fmt.Sprintf("priority-card-test-%s", randomSuffix),
			})
			fmt.Printf("测试9: 测试上下文初始化完成，命名空间 %s\n", ctx.Namespace)
			defer e2eutil.CleanupTestContext(ctx)

			// 创建高优先级队列 - 用于H800卡片
			priorityQueueSpec := &e2eutil.QueueSpec{
				Name:   fmt.Sprintf("high-priority-queue-%s", randomSuffix),
				Weight: 100, // 高权重
				Capacity: v1.ResourceList{
					v1.ResourceCPU:    resource.MustParse("2"),
					v1.ResourceMemory: resource.MustParse("2Gi"),
				},
				Annotations: map[string]string{
					"volcano.sh/card.quota": fmt.Sprintf(`{"%s": 4}`, CardTypeH800),
				},
			}

			// 使用e2eutil函数创建高优先级队列
			fmt.Printf("测试9: 开始创建高优先级队列 %s\n", priorityQueueSpec.Name)
			e2eutil.CreateQueueWithQueueSpec(ctx, priorityQueueSpec)
			fmt.Printf("测试9: 高优先级队列 %s 创建成功，%s卡片配额为4\n", priorityQueueSpec.Name, CardTypeH800)

			// 创建低优先级队列 - 用于RTX4090卡片
			normalQueueSpec := &e2eutil.QueueSpec{
				Name:   fmt.Sprintf("normal-priority-queue-%s", randomSuffix),
				Weight: 10, // 低权重
				Capacity: v1.ResourceList{
					v1.ResourceCPU:    resource.MustParse("2"),
					v1.ResourceMemory: resource.MustParse("2Gi"),
				},
				Annotations: map[string]string{
					"volcano.sh/card.quota": fmt.Sprintf(`{"%s": 4}`, CardTypeRTX4090),
				},
			}

			// 使用e2eutil函数创建低优先级队列
			fmt.Printf("测试9: 开始创建低优先级队列 %s\n", normalQueueSpec.Name)
			e2eutil.CreateQueueWithQueueSpec(ctx, normalQueueSpec)
			fmt.Printf("测试9: 低优先级队列 %s 创建成功，%s卡片配额为4\n", normalQueueSpec.Name, CardTypeRTX4090)

			// 等待高优先级队列状态变为开放
			fmt.Printf("测试9: 等待高优先级队列 %s 状态变为开放\n", priorityQueueSpec.Name)
			queueOpenErr := e2eutil.WaitQueueStatus(func() (bool, error) {
				queue, err := ctx.Vcclient.SchedulingV1beta1().Queues().Get(context.TODO(), priorityQueueSpec.Name, metav1.GetOptions{})
				if err != nil {
					return false, err
				}
				return queue.Status.State == schedulingv1beta1.QueueStateOpen, nil
			})
			Expect(queueOpenErr).NotTo(HaveOccurred(), "高优先级队列未能在超时时间内变为开放状态")

			// 等待低优先级队列状态变为开放
			fmt.Printf("测试9: 等待低优先级队列 %s 状态变为开放\n", normalQueueSpec.Name)
			queueOpenErr = e2eutil.WaitQueueStatus(func() (bool, error) {
				queue, err := ctx.Vcclient.SchedulingV1beta1().Queues().Get(context.TODO(), normalQueueSpec.Name, metav1.GetOptions{})
				if err != nil {
					return false, err
				}
				return queue.Status.State == schedulingv1beta1.QueueStateOpen, nil
			})
			Expect(queueOpenErr).NotTo(HaveOccurred(), "低优先级队列未能在超时时间内变为开放状态")

			fmt.Printf("测试9: 两个队列状态已变为开放\n")

			// 清理资源
			defer func() {
				// 使用e2eutil删除队列
				fmt.Printf("测试9: 清理高优先级队列 %s\n", priorityQueueSpec.Name)
				e2eutil.DeleteQueue(ctx, priorityQueueSpec.Name)

				fmt.Printf("测试9: 清理低优先级队列 %s\n", normalQueueSpec.Name)
				e2eutil.DeleteQueue(ctx, normalQueueSpec.Name)
			}()

			// 创建高优先级作业（H800卡片）
			highPriorityJobSpec := &e2eutil.JobSpec{
				Name:  fmt.Sprintf("high-priority-job-%s", randomSuffix),
				Queue: priorityQueueSpec.Name,
				Tasks: []e2eutil.TaskSpec{
					{
						Name: "task",
						Min:  1,
						Rep:  1,
						Img:  e2eutil.DefaultNginxImage,
						Req: v1.ResourceList{
							v1.ResourceCPU:                    resource.MustParse("1"),
							v1.ResourceMemory:                 resource.MustParse("1Gi"),
							v1.ResourceName("nvidia.com/gpu"): resource.MustParse("1"),
						},
						Limit: v1.ResourceList{
							v1.ResourceCPU:                    resource.MustParse("1"),
							v1.ResourceMemory:                 resource.MustParse("1Gi"),
							v1.ResourceName("nvidia.com/gpu"): resource.MustParse("1"),
						},
						Annotations: map[string]string{
							"volcano.sh/card.name": CardTypeH800,
						},
					},
				},
				Annotations: map[string]string{
					"volcano.sh/card.request": fmt.Sprintf(`{"%s": 1}`, CardTypeH800),
					"volcano.sh/job.priority": "high",
				},
			}

			// 直接创建高优先级作业（不使用PodGroup）
			fmt.Printf("测试9: 开始创建高优先级作业 %s\n", highPriorityJobSpec.Name)
			highPriorityJob := e2eutil.CreateJob(ctx, highPriorityJobSpec)
			fmt.Printf("测试9: 高优先级作业 %s 创建成功，请求%s卡片资源\n", highPriorityJob.Name, CardTypeH800)

			// 创建低优先级作业（RTX4090卡片）
			normalPriorityJobSpec := &e2eutil.JobSpec{
				Name:  fmt.Sprintf("normal-priority-job-%s", randomSuffix),
				Queue: normalQueueSpec.Name,
				Tasks: []e2eutil.TaskSpec{
					{
						Name: "task",
						Min:  1,
						Rep:  1,
						Img:  e2eutil.DefaultNginxImage,
						Req: v1.ResourceList{
							v1.ResourceCPU:                    resource.MustParse("1"),
							v1.ResourceMemory:                 resource.MustParse("1Gi"),
							v1.ResourceName("nvidia.com/gpu"): resource.MustParse("1"),
						},
						Limit: v1.ResourceList{
							v1.ResourceCPU:                    resource.MustParse("1"),
							v1.ResourceMemory:                 resource.MustParse("1Gi"),
							v1.ResourceName("nvidia.com/gpu"): resource.MustParse("1"),
						},
						Annotations: map[string]string{
							"volcano.sh/card.name": CardTypeRTX4090,
						},
					},
				},
				Annotations: map[string]string{
					"volcano.sh/card.request": fmt.Sprintf(`{"%s": 1}`, CardTypeRTX4090),
					"volcano.sh/job.priority": "normal",
				},
			}

			// 直接创建低优先级作业（不使用PodGroup）
			fmt.Printf("测试9: 开始创建低优先级作业 %s\n", normalPriorityJobSpec.Name)
			normalPriorityJob := e2eutil.CreateJob(ctx, normalPriorityJobSpec)
			fmt.Printf("测试9: 低优先级作业 %s 创建成功，请求%s卡片资源\n", normalPriorityJob.Name, CardTypeRTX4090)

			// 等待高优先级作业就绪
			fmt.Printf("测试9: 等待高优先级作业就绪\n")
			err := e2eutil.WaitJobReady(ctx, highPriorityJob)
			Expect(err).NotTo(HaveOccurred(), "高优先级作业未能在超时时间内就绪")
			fmt.Printf("测试9: 高优先级作业 %s 已就绪\n", highPriorityJob.Name)

			// 等待低优先级作业就绪
			fmt.Printf("测试9: 等待低优先级作业就绪\n")
			err = e2eutil.WaitJobReady(ctx, normalPriorityJob)
			Expect(err).NotTo(HaveOccurred(), "低优先级作业未能在超时时间内就绪")
			fmt.Printf("测试9: 低优先级作业 %s 已就绪\n", normalPriorityJob.Name)

			// 清理资源
			defer func() {
				// 删除作业
				e2eutil.DeleteJob(ctx, highPriorityJob)
				fmt.Printf("测试9: 高优先级作业 %s 已清理\n", highPriorityJob.Name)
				e2eutil.DeleteJob(ctx, normalPriorityJob)
				fmt.Printf("测试9: 低优先级作业 %s 已清理\n", normalPriorityJob.Name)
			}()
		})

		// 测试10: CPU内存无限配额下混合作业测试
		It("Mixed Jobs with CardUnlimitedCpuMemory", func() {
			randomSuffix := generateRandomSuffix()
			fmt.Printf("测试10: 生成随机后缀 %s\n", randomSuffix)
			ctx := e2eutil.InitTestContext(e2eutil.Options{
				Namespace: fmt.Sprintf("mixed-jobs-test-%s", randomSuffix),
			})
			fmt.Printf("测试10: 测试上下文初始化完成，命名空间 %s\n", ctx.Namespace)
			defer e2eutil.CleanupTestContext(ctx)

			// 创建带卡片配额和cardUnlimitedCpuMemory=true的队列
			queueSpec := &e2eutil.QueueSpec{
				Name:   fmt.Sprintf("mixed-jobs-queue-%s", randomSuffix),
				Weight: 10,
				Capacity: v1.ResourceList{
					v1.ResourceCPU:    resource.MustParse("2"),
					v1.ResourceMemory: resource.MustParse("2Gi"),
				},
				Annotations: map[string]string{
					"volcano.sh/card.quota": fmt.Sprintf(`{"%s": 4}`, CardTypeTeslaK80),
				},
			}

			// 使用e2eutil函数创建队列
			fmt.Printf("测试10: 开始创建队列 %s\n", queueSpec.Name)
			e2eutil.CreateQueueWithQueueSpec(ctx, queueSpec)
			fmt.Printf("测试10: 队列 %s 创建成功，配置Tesla-K80卡片配额4，CPU/内存无限\n", queueSpec.Name)

			// 等待队列状态变为开放
			fmt.Printf("测试10: 等待队列 %s 状态变为开放\n", queueSpec.Name)
			queueOpenErr := e2eutil.WaitQueueStatus(func() (bool, error) {
				queue, err := ctx.Vcclient.SchedulingV1beta1().Queues().Get(context.TODO(), queueSpec.Name, metav1.GetOptions{})
				if err != nil {
					return false, err
				}
				return queue.Status.State == schedulingv1beta1.QueueStateOpen, nil
			})
			Expect(queueOpenErr).NotTo(HaveOccurred(), "队列未能在超时时间内变为开放状态")
			fmt.Printf("测试10: 队列 %s 状态已变为开放\n", queueSpec.Name)

			// 清理资源
			defer func() {
				// 使用e2eutil删除队列
				fmt.Printf("测试10: 清理队列 %s\n", queueSpec.Name)
				e2eutil.DeleteQueue(ctx, queueSpec.Name)
			}()

			// 1. 创建一个纯CPU作业
			cpuJobSpec := &e2eutil.JobSpec{
				Name:  fmt.Sprintf("cpu-only-job-%s", randomSuffix),
				Queue: queueSpec.Name,
				Tasks: []e2eutil.TaskSpec{
					{
						Name: "cpu-task",
						Min:  1,
						Rep:  2,
						Img:  e2eutil.DefaultNginxImage,
						Req: v1.ResourceList{
							v1.ResourceCPU:    resource.MustParse("2"),
							v1.ResourceMemory: resource.MustParse("2Gi"),
						},
					},
				},
			}

			// 创建纯CPU作业
			fmt.Printf("测试10: 开始创建纯CPU作业 %s\n", cpuJobSpec.Name)
			cpuJob := e2eutil.CreateJob(ctx, cpuJobSpec)
			fmt.Printf("测试10: 纯CPU作业 %s 创建成功\n", cpuJob.Name)

			// 2. 创建一个具有卡片请求的作业
			cardJobSpec := &e2eutil.JobSpec{
				Name:  fmt.Sprintf("card-job-%s", randomSuffix),
				Queue: queueSpec.Name,
				Tasks: []e2eutil.TaskSpec{
					{
						Name: "card-task",
						Min:  1,
						Rep:  1,
						Img:  e2eutil.DefaultNginxImage,
						Req: v1.ResourceList{
							v1.ResourceCPU:                    resource.MustParse("1"),
							v1.ResourceMemory:                 resource.MustParse("1Gi"),
							v1.ResourceName("nvidia.com/gpu"): resource.MustParse("2"),
						},
						Limit: v1.ResourceList{
							v1.ResourceCPU:                    resource.MustParse("1"),
							v1.ResourceMemory:                 resource.MustParse("1Gi"),
							v1.ResourceName("nvidia.com/gpu"): resource.MustParse("2"),
						},
						Annotations: map[string]string{
							"volcano.sh/card.name": CardTypeTeslaK80,
						},
					},
				},
				Annotations: map[string]string{
					"volcano.sh/card.request": fmt.Sprintf(`{"%s": 2}`, CardTypeTeslaK80),
				},
			}

			// 创建带卡片请求的作业
			fmt.Printf("测试10: 开始创建带卡片请求的作业 %s\n", cardJobSpec.Name)
			cardJob := e2eutil.CreateJob(ctx, cardJobSpec)
			fmt.Printf("测试10: 带卡片请求作业 %s 创建成功，请求Tesla-K80卡片2张\n", cardJob.Name)

			// 3. 创建一个超额纯CPU作业
			overCpuJobSpec := &e2eutil.JobSpec{
				Name:  fmt.Sprintf("over-cpu-job-%s", randomSuffix),
				Queue: queueSpec.Name,
				Tasks: []e2eutil.TaskSpec{
					{
						Name: "over-cpu-task",
						Min:  1,
						Rep:  2,
						Img:  e2eutil.DefaultNginxImage,
						Req: v1.ResourceList{
							v1.ResourceCPU:    resource.MustParse("2"),
							v1.ResourceMemory: resource.MustParse("2Gi"),
						},
					},
				},
			}

			// 创建纯CPU作业
			fmt.Printf("测试10: 开始创建超额纯CPU作业 %s\n", overCpuJobSpec.Name)
			overCpuJob := e2eutil.CreateJob(ctx, overCpuJobSpec)
			fmt.Printf("测试10: 超额纯CPU作业 %s 创建成功\n", overCpuJob.Name)

			// 等待纯CPU作业就绪
			fmt.Printf("测试10: 等待纯CPU作业就绪\n")
			err := e2eutil.WaitJobReady(ctx, cpuJob)
			Expect(err).NotTo(HaveOccurred(), "纯CPU作业未能在超时时间内就绪")
			fmt.Printf("测试10: 纯CPU作业 %s 已就绪\n", cpuJob.Name)

			// 等待带卡片请求的作业就绪
			fmt.Printf("测试10: 等待带卡片请求的作业就绪\n")
			err = e2eutil.WaitJobReady(ctx, cardJob)
			Expect(err).NotTo(HaveOccurred(), "带卡片请求作业未能在超时时间内就绪")
			fmt.Printf("测试10: 带卡片请求作业 %s 已就绪\n", cardJob.Name)

			// 等待超额纯CPU作业调度失败
			fmt.Printf("测试10: 等待超额纯CPU作业调度失败\n")
			updatedJob, err := ctx.Vcclient.BatchV1alpha1().Jobs(ctx.Namespace).Get(context.TODO(), overCpuJob.Name, metav1.GetOptions{})
			Expect(err).NotTo(HaveOccurred(), "获取作业状态失败")
			fmt.Printf("测试10: 作业当前状态: %s, Running: %d, Pending: %d, Failed: %d\n",
				updatedJob.Status.State, updatedJob.Status.Running, updatedJob.Status.Pending, updatedJob.Status.Failed)

			// 检查关联的Pod
			pods, err := ctx.Kubeclient.CoreV1().Pods(ctx.Namespace).List(context.TODO(), metav1.ListOptions{
				LabelSelector: fmt.Sprintf("volcano.sh/job-name=%s", overCpuJob.Name),
			})
			Expect(err).NotTo(HaveOccurred(), "获取Pod列表失败")

			// 打印Pod状态信息
			for _, pod := range pods.Items {
				fmt.Printf("测试10: Pod %s 状态: %s\n", pod.Name, pod.Status.Phase)
			}

			// 校验作业状态：应该没有运行中的Pod（因为无资源配额）
			Expect(updatedJob.Status.Running).To(Equal(int32(0)), "作业不应该有运行中的Pod，因为超额")

			// 校验作业状态：作业应该处于Pending状态
			Expect(updatedJob.Status.State.Phase).To(Equal(batchv1alpha1.Pending), "作业应该处于Pending状态，因为超额")

			// 校验Pod状态：所有Pod都应该处于Pending状态（无法调度）
			for _, pod := range pods.Items {
				Expect(pod.Status.Phase).To(Equal(v1.PodPending), "Pod应该处于Pending状态，因为超额")
			}

			// 校验Pod调度条件：Pod应该有Unschedulable条件
			for _, pod := range pods.Items {
				hasUnschedulableCondition := false
				for _, condition := range pod.Status.Conditions {
					if condition.Type == v1.PodScheduled && condition.Status == v1.ConditionFalse && condition.Reason == "Unschedulable" {
						hasUnschedulableCondition = true
						break
					}
				}
				Expect(hasUnschedulableCondition).To(BeTrue(), "Pod应该有Unschedulable条件，表示超额")
			}

			// 清理资源
			defer func() {
				// 删除作业
				e2eutil.DeleteJob(ctx, cpuJob)
				fmt.Printf("测试10: 纯CPU作业 %s 已清理\n", cpuJob.Name)
				e2eutil.DeleteJob(ctx, cardJob)
				fmt.Printf("测试10: 带卡片请求作业 %s 已清理\n", cardJob.Name)
			}()
		})

		// 测试11: 无资源配额队列调度限制测试
		It("No Resource Quota Queue Scheduling Restriction", func() {
			randomSuffix := generateRandomSuffix()
			fmt.Printf("测试11: 生成随机后缀 %s\n", randomSuffix)
			ctx := e2eutil.InitTestContext(e2eutil.Options{
				Namespace: fmt.Sprintf("no-quota-test-%s", randomSuffix),
			})
			fmt.Printf("测试11: 测试上下文初始化完成，命名空间 %s\n", ctx.Namespace)
			defer e2eutil.CleanupTestContext(ctx)

			// 创建无任何资源配额的队列（不设置卡配额、CPU和内存保证）
			queueSpec := &e2eutil.QueueSpec{
				Name:        fmt.Sprintf("no-quota-queue-%s", randomSuffix),
				Weight:      10,
				Annotations: map[string]string{},
			}

			// 使用e2eutil函数创建队列
			fmt.Printf("测试11: 开始创建无资源配额队列 %s\n", queueSpec.Name)
			e2eutil.CreateQueueWithQueueSpec(ctx, queueSpec)
			fmt.Printf("测试11: 无资源配额队列 %s 创建成功\n", queueSpec.Name)

			// 等待队列状态变为开放
			fmt.Printf("测试11: 等待队列 %s 状态变为开放\n", queueSpec.Name)
			queueOpenErr := e2eutil.WaitQueueStatus(func() (bool, error) {
				queue, err := ctx.Vcclient.SchedulingV1beta1().Queues().Get(context.TODO(), queueSpec.Name, metav1.GetOptions{})
				if err != nil {
					return false, err
				}
				return queue.Status.State == schedulingv1beta1.QueueStateOpen, nil
			})
			Expect(queueOpenErr).NotTo(HaveOccurred(), "队列未能在超时时间内变为开放状态")
			fmt.Printf("测试11: 队列 %s 状态已变为开放\n", queueSpec.Name)

			// 清理资源
			defer func() {
				// 使用e2eutil删除队列
				fmt.Printf("测试11: 清理队列 %s\n", queueSpec.Name)
				e2eutil.DeleteQueue(ctx, queueSpec.Name)
			}()

			// 创建一个作业，尝试调度到无资源配额的队列
			jobSpec := &e2eutil.JobSpec{
				Name:  fmt.Sprintf("test-job-%s", randomSuffix),
				Queue: queueSpec.Name,
				Tasks: []e2eutil.TaskSpec{
					{
						Name: "task",
						Min:  1,
						Rep:  1,
						Img:  e2eutil.DefaultNginxImage,
						Req: v1.ResourceList{
							v1.ResourceCPU:    resource.MustParse("1"),
							v1.ResourceMemory: resource.MustParse("1Gi"),
						},
					},
				},
			}

			// 直接创建作业（不使用PodGroup），并设置卡片请求的annotations
			fmt.Printf("测试11: 开始创建测试作业 %s\n", jobSpec.Name)
			job := e2eutil.CreateJob(ctx, jobSpec)
			fmt.Printf("测试11: 测试作业 %s 创建成功，尝试调度到无资源配额队列\n", job.Name)

			// 创建一个卡作业，尝试调度到无资源配额的队列
			cardJobSpec := &e2eutil.JobSpec{
				Name:  fmt.Sprintf("test-card-job-%s", randomSuffix),
				Queue: queueSpec.Name,
				Tasks: []e2eutil.TaskSpec{
					{
						Name: "card-task",
						Min:  1,
						Rep:  1,
						Img:  e2eutil.DefaultNginxImage,
						Req: v1.ResourceList{
							v1.ResourceCPU:                    resource.MustParse("1"),
							v1.ResourceMemory:                 resource.MustParse("1Gi"),
							v1.ResourceName("nvidia.com/gpu"): resource.MustParse("1"),
						},
						Limit: v1.ResourceList{
							v1.ResourceCPU:                    resource.MustParse("1"),
							v1.ResourceMemory:                 resource.MustParse("1Gi"),
							v1.ResourceName("nvidia.com/gpu"): resource.MustParse("1"),
						},
						Annotations: map[string]string{
							"volcano.sh/card.name": CardTypeRTX4090,
						},
					},
				},
				Annotations: map[string]string{
					"volcano.sh/card.request": fmt.Sprintf(`{"%s": 1}`, CardTypeRTX4090),
				},
			}

			// 直接创建作业（不使用PodGroup），并设置卡片请求的annotations
			fmt.Printf("测试11: 开始创建测试作业 %s\n", cardJobSpec.Name)
			cardJob := e2eutil.CreateJob(ctx, cardJobSpec)
			fmt.Printf("测试11: 测试作业 %s 创建成功，尝试调度到无资源配额队列\n", cardJob.Name)

			// 等待一段时间，让调度器尝试调度
			fmt.Printf("测试11: 等待调度器处理作业，超时时间为 %v\n", JobProcessTimeout/2)
			time.Sleep(JobProcessTimeout / 2)
			fmt.Printf("测试11: 调度器处理时间结束\n")

			// 检查作业状态（应该没有被调度）
			fmt.Printf("测试11: 检查作业状态\n")
			updatedJob, err := ctx.Vcclient.BatchV1alpha1().Jobs(ctx.Namespace).Get(context.TODO(), job.Name, metav1.GetOptions{})
			Expect(err).NotTo(HaveOccurred(), "获取作业状态失败")
			fmt.Printf("测试11: 作业当前状态: %s, Running: %d, Pending: %d, Failed: %d\n",
				updatedJob.Status.State, updatedJob.Status.Running, updatedJob.Status.Pending, updatedJob.Status.Failed)

			// 检查关联的Pod
			pods, err := ctx.Kubeclient.CoreV1().Pods(ctx.Namespace).List(context.TODO(), metav1.ListOptions{
				LabelSelector: fmt.Sprintf("volcano.sh/job-name=%s", job.Name),
			})
			Expect(err).NotTo(HaveOccurred(), "获取Pod列表失败")

			// 打印Pod状态信息
			for _, pod := range pods.Items {
				fmt.Printf("测试11: Pod %s 状态: %s\n", pod.Name, pod.Status.Phase)
			}

			// 校验作业状态：应该没有运行中的Pod（因为无资源配额）
			Expect(updatedJob.Status.Running).To(Equal(int32(0)), "作业不应该有运行中的Pod，因为队列没有资源配额")

			// 校验作业状态：作业应该处于Pending状态
			Expect(updatedJob.Status.State.Phase).To(Equal(batchv1alpha1.Pending), "作业应该处于Pending状态，因为无法调度")

			// 校验Pod状态：所有Pod都应该处于Pending状态（无法调度）
			for _, pod := range pods.Items {
				Expect(pod.Status.Phase).To(Equal(v1.PodPending), "Pod应该处于Pending状态，因为资源配额不足")
			}

			// 校验Pod调度条件：Pod应该有Unschedulable条件
			for _, pod := range pods.Items {
				hasUnschedulableCondition := false
				for _, condition := range pod.Status.Conditions {
					if condition.Type == v1.PodScheduled && condition.Status == v1.ConditionFalse && condition.Reason == "Unschedulable" {
						hasUnschedulableCondition = true
						break
					}
				}
				Expect(hasUnschedulableCondition).To(BeTrue(), "Pod应该有Unschedulable条件，表示无法调度")
			}

			// 检查卡片作业状态（应该没有被调度）
			fmt.Printf("测试11: 检查卡片作业状态\n")
			updatedCardJob, err := ctx.Vcclient.BatchV1alpha1().Jobs(ctx.Namespace).Get(context.TODO(), cardJob.Name, metav1.GetOptions{})
			Expect(err).NotTo(HaveOccurred(), "获取卡片作业状态失败")
			fmt.Printf("测试11: 卡片作业当前状态: %s, Running: %d, Pending: %d, Failed: %d\n",
				updatedCardJob.Status.State, updatedCardJob.Status.Running, updatedCardJob.Status.Pending, updatedCardJob.Status.Failed)

			// 检查卡片作业关联的Pod
			cardPods, err := ctx.Kubeclient.CoreV1().Pods(ctx.Namespace).List(context.TODO(), metav1.ListOptions{
				LabelSelector: fmt.Sprintf("volcano.sh/job-name=%s", cardJob.Name),
			})
			Expect(err).NotTo(HaveOccurred(), "获取卡片作业Pod列表失败")

			// 打印卡片作业Pod状态信息
			for _, pod := range cardPods.Items {
				fmt.Printf("测试11: 卡片作业Pod %s 状态: %s\n", pod.Name, pod.Status.Phase)
			}

			// 校验卡片作业状态：应该没有运行中的Pod（因为无资源配额）
			Expect(updatedCardJob.Status.Running).To(Equal(int32(0)), "卡片作业不应该有运行中的Pod，因为队列没有资源配额")

			// 校验卡片作业状态：卡片作业应该处于Pending状态
			Expect(updatedCardJob.Status.State.Phase).To(Equal(batchv1alpha1.Pending), "卡片作业应该处于Pending状态，因为无法调度")

			// 校验卡片作业Pod状态：所有卡片作业Pod都应该处于Pending状态（无法调度）
			for _, pod := range cardPods.Items {
				Expect(pod.Status.Phase).To(Equal(v1.PodPending), "卡片作业Pod应该处于Pending状态，因为资源配额不足")
			}

			// 校验卡片作业Pod调度条件：卡片作业Pod应该有Unschedulable条件
			for _, pod := range cardPods.Items {
				hasUnschedulableCondition := false
				for _, condition := range pod.Status.Conditions {
					if condition.Type == v1.PodScheduled && condition.Status == v1.ConditionFalse && condition.Reason == "Unschedulable" {
						hasUnschedulableCondition = true
						break
					}
				}
				Expect(hasUnschedulableCondition).To(BeTrue(), "卡片作业Pod应该有Unschedulable条件，表示无法调度")
			}

			fmt.Printf("测试11: 验证通过 - 作业正确地被拒绝在无资源配额队列中调度\n")

			// 清理资源
			defer func() {
				// 删除作业
				e2eutil.DeleteJob(ctx, job)
				fmt.Printf("测试11: 作业 %s 已清理\n", job.Name)
			}()
		})
	})

	Context("Capacity Card - Deployment", func() {
		// 测试12: Deployment GPU卡片资源分配测试
		It("Deployment GPU Card Resource Allocation", func() {
			randomSuffix := generateRandomSuffix()
			fmt.Printf("测试12: 生成随机后缀 %s\n", randomSuffix)
			ctx := e2eutil.InitTestContext(e2eutil.Options{
				Namespace: fmt.Sprintf("deployment-card-test-%s", randomSuffix),
			})
			fmt.Printf("测试12: 测试上下文初始化完成，命名空间 %s\n", ctx.Namespace)
			defer e2eutil.CleanupTestContext(ctx)

			// 创建带卡片配额的队列
			queueSpec := &e2eutil.QueueSpec{
				Name:   fmt.Sprintf("deployment-queue-%s", randomSuffix),
				Weight: 10,
				Capacity: v1.ResourceList{
					v1.ResourceCPU:    resource.MustParse("2"),
					v1.ResourceMemory: resource.MustParse("2Gi"),
				},
				Annotations: map[string]string{
					"volcano.sh/card.quota": fmt.Sprintf(`{"%s": 2}`, CardTypeRTX4090),
				},
			}

			// 使用e2eutil创建队列
			fmt.Printf("测试12: 开始创建队列 %s\n", queueSpec.Name)
			e2eutil.CreateQueueWithQueueSpec(ctx, queueSpec)
			fmt.Printf("测试12: 队列 %s 创建成功，RTX4090卡片配额为2\n", queueSpec.Name)

			// 等待队列状态变为开放
			fmt.Printf("测试12: 等待队列 %s 状态变为开放\n", queueSpec.Name)
			queueOpenErr := e2eutil.WaitQueueStatus(func() (bool, error) {
				queue, err := ctx.Vcclient.SchedulingV1beta1().Queues().Get(context.TODO(), queueSpec.Name, metav1.GetOptions{})
				if err != nil {
					return false, err
				}
				return queue.Status.State == schedulingv1beta1.QueueStateOpen, nil
			})
			Expect(queueOpenErr).NotTo(HaveOccurred(), "队列未能在超时时间内变为开放状态")
			fmt.Printf("测试12: 队列 %s 状态已变为开放\n", queueSpec.Name)

			// 创建Deployment并添加GPU卡片请求注解
			deploymentName := fmt.Sprintf("gpu-card-deployment-%s", randomSuffix)
			deployment := &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					Name:      deploymentName,
					Namespace: ctx.Namespace,
				},
				Spec: appsv1.DeploymentSpec{
					Replicas: int32Ptr(1),
					Selector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"app": deploymentName,
						},
					},
					Template: v1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{
							Labels: map[string]string{
								"app": deploymentName,
							},
							Annotations: map[string]string{
								"volcano.sh/card.name":             CardTypeRTX4090,
								"scheduling.volcano.sh/queue-name": queueSpec.Name,
							},
						},
						Spec: v1.PodSpec{
							Containers: []v1.Container{
								{
									Name:  "nginx",
									Image: e2eutil.DefaultNginxImage,
									Resources: v1.ResourceRequirements{
										Requests: v1.ResourceList{
											v1.ResourceCPU:                    resource.MustParse("1"),
											v1.ResourceMemory:                 resource.MustParse("1Gi"),
											v1.ResourceName("nvidia.com/gpu"): resource.MustParse("1"),
										},
										Limits: v1.ResourceList{
											v1.ResourceCPU:                    resource.MustParse("1"),
											v1.ResourceMemory:                 resource.MustParse("1Gi"),
											v1.ResourceName("nvidia.com/gpu"): resource.MustParse("1"),
										},
									},
								},
							},
							SchedulerName: "volcano",
						},
					},
				},
			}

			// 创建Deployment
			fmt.Printf("测试12: 开始创建Deployment %s\n", deploymentName)
			_, err := ctx.Kubeclient.AppsV1().Deployments(ctx.Namespace).Create(context.TODO(), deployment, metav1.CreateOptions{})
			Expect(err).NotTo(HaveOccurred())
			fmt.Printf("测试12: Deployment %s 创建成功，请求RTX4090卡片资源\n", deploymentName)

			// 等待Deployment就绪
			fmt.Printf("测试12: 等待Deployment就绪\n")
			err = e2eutil.WaitDeploymentReady(ctx, deploymentName)
			Expect(err).NotTo(HaveOccurred(), "Deployment未能在超时时间内就绪")
			fmt.Printf("测试12: Deployment %s 已就绪\n", deploymentName)

			// 清理资源
			defer func() {
				fmt.Printf("测试12: 开始清理资源\n")
				// 删除Deployment
				fmt.Printf("测试12: 删除Deployment %s\n", deploymentName)
				if err := ctx.Kubeclient.AppsV1().Deployments(ctx.Namespace).Delete(context.TODO(), deploymentName, metav1.DeleteOptions{}); err != nil {
					fmt.Printf("测试12-警告：删除Deployment %s 失败: %v\n", deploymentName, err)
				} else {
					fmt.Printf("测试12: Deployment %s 删除成功\n", deploymentName)
				}

				// 使用e2eutil删除队列
				fmt.Printf("测试12: 删除队列 %s\n", queueSpec.Name)
				e2eutil.DeleteQueue(ctx, queueSpec.Name)
				fmt.Printf("测试12: 资源清理完成\n")
			}()
		})
	})
})

// 辅助函数：int32指针
func int32Ptr(i int32) *int32 {
	return &i
}
