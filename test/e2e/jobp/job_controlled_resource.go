/*
Copyright 2021 The Volcano Authors.

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

package jobp

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	v12 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"volcano.sh/apis/pkg/apis/batch/v1alpha1"

	e2eutil "volcano.sh/volcano/test/e2e/util"
)

var _ = Describe("Job E2E Test: Test Job PVCs", func() {
	It("use exisisting PVC in job", func() {
		jobName := "job-pvc-name-exist"
		taskName := "pvctask"
		pvName := "job-pv-name"
		pvcName := "job-pvc-name-exist"
		ctx := e2eutil.InitTestContext(e2eutil.Options{})
		defer e2eutil.CleanupTestContext(ctx)

		var tt v12.HostPathType = "DirectoryOrCreate"

		storageClsName := "standard"

		// create pv
		pv := v12.PersistentVolume{
			ObjectMeta: metav1.ObjectMeta{
				Name: pvName,
			},
			Spec: v12.PersistentVolumeSpec{
				StorageClassName: storageClsName,
				Capacity: v12.ResourceList{
					v12.ResourceName(v12.ResourceStorage): resource.MustParse("1Gi"),
				},
				PersistentVolumeSource: v12.PersistentVolumeSource{
					HostPath: &v12.HostPathVolumeSource{
						Path: "/tmp/pvtest",
						Type: &tt,
					}},
				AccessModes: []v12.PersistentVolumeAccessMode{
					v12.ReadWriteOnce,
				},
			},
		}
		_, err := ctx.Kubeclient.CoreV1().PersistentVolumes().Create(context.TODO(), &pv, metav1.CreateOptions{})
		Expect(err).NotTo(HaveOccurred(), "pv creation ")
		// create pvc
		pvc := v12.PersistentVolumeClaim{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: ctx.Namespace,
				Name:      pvcName,
			},
			Spec: v12.PersistentVolumeClaimSpec{
				StorageClassName: &storageClsName,
				VolumeName:       pvName,
				Resources: v12.ResourceRequirements{
					Requests: v12.ResourceList{
						v12.ResourceName(v12.ResourceStorage): resource.MustParse("1Gi"),
					},
				},
				AccessModes: []v12.PersistentVolumeAccessMode{
					v12.ReadWriteOnce,
				},
			},
		}
		_, err1 := ctx.Kubeclient.CoreV1().PersistentVolumeClaims(ctx.Namespace).Create(context.TODO(), &pvc, metav1.CreateOptions{})
		Expect(err1).NotTo(HaveOccurred(), "pvc creation")

		pvSpec := &v12.PersistentVolumeClaimSpec{
			Resources: v12.ResourceRequirements{
				Requests: v12.ResourceList{
					v12.ResourceName(v12.ResourceStorage): resource.MustParse("1Gi"),
				},
			},
			AccessModes: []v12.PersistentVolumeAccessMode{
				v12.ReadWriteOnce,
			},
		}
		job := e2eutil.CreateJob(ctx, &e2eutil.JobSpec{
			Namespace: ctx.Namespace,
			Name:      jobName,
			Tasks: []e2eutil.TaskSpec{
				{
					Img:  e2eutil.DefaultNginxImage,
					Req:  e2eutil.HalfCPU,
					Min:  1,
					Rep:  1,
					Name: taskName,
				},
			},
			Volumes: []v1alpha1.VolumeSpec{
				{
					MountPath:       "/mountone",
					VolumeClaimName: pvcName,
				},
				{
					MountPath:   "/mounttwo",
					VolumeClaim: pvSpec,
				},
			},
		})

		err = e2eutil.WaitJobReady(ctx, job)
		Expect(err).NotTo(HaveOccurred())

		job, err = ctx.Vcclient.BatchV1alpha1().Jobs(ctx.Namespace).Get(context.TODO(), jobName, metav1.GetOptions{})
		Expect(err).NotTo(HaveOccurred())

		Expect(len(job.Spec.Volumes)).To(Equal(2),
			" volume should be created")
		Expect(job.Spec.Volumes[0].VolumeClaimName).Should(Equal(pvcName),
			"volume 1 PVC name should not be generated .")
		Expect(job.Spec.Volumes[1].VolumeClaimName).Should(Not(Equal("")),
			"volume 0 PVC name should be generated.")
	})

	It("Generate PodGroup and valid minResource when creating job", func() {
		jobName := "job-name-podgroup"
		ctx := e2eutil.InitTestContext(e2eutil.Options{})
		defer e2eutil.CleanupTestContext(ctx)

		resource := v12.ResourceList{
			"cpu":            resource.MustParse("1000m"),
			"memory":         resource.MustParse("1000Mi"),
			"nvidia.com/gpu": resource.MustParse("1"),
		}

		job := e2eutil.CreateJob(ctx, &e2eutil.JobSpec{
			Namespace: ctx.Namespace,
			Name:      jobName,
			Tasks: []e2eutil.TaskSpec{
				{
					Img:   e2eutil.DefaultNginxImage,
					Min:   1,
					Rep:   1,
					Name:  "task-1",
					Req:   resource,
					Limit: resource,
				},
				{
					Img:   e2eutil.DefaultNginxImage,
					Min:   1,
					Rep:   1,
					Name:  "task-2",
					Req:   resource,
					Limit: resource,
				},
			},
		})

		expected := map[string]int64{
			"count/pods":              2,
			"cpu":                     2,
			"memory":                  1024 * 1024 * 2000,
			"nvidia.com/gpu":          2,
			"limits.cpu":              2,
			"limits.memory":           1024 * 1024 * 2000,
			"requests.memory":         1024 * 1024 * 2000,
			"requests.nvidia.com/gpu": 2,
			"pods":                    2,
			"requests.cpu":            2,
		}

		err := e2eutil.WaitJobStatePending(ctx, job)
		Expect(err).NotTo(HaveOccurred())

		pgName := jobName + "-" + string(job.UID)
		pGroup, err := ctx.Vcclient.SchedulingV1beta1().PodGroups(ctx.Namespace).Get(context.TODO(), pgName, metav1.GetOptions{})
		Expect(err).NotTo(HaveOccurred())

		for name, q := range *pGroup.Spec.MinResources {
			value, ok := expected[string(name)]
			Expect(ok).To(Equal(true), "Resource %s should exists in PodGroup", name)
			Expect(q.Value()).To(Equal(value), "Resource %s 's value should equal to %d", name, value)
		}

	})
})
