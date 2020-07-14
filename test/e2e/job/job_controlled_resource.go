/*
Copyright 2019 The Volcano Authors.

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

package job

import (
	"context"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	v12 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"volcano.sh/volcano/pkg/apis/batch/v1alpha1"
)

var _ = Describe("Job E2E Test: Test Job PVCs", func() {
	It("use exisisting PVC in job", func() {
		jobName := "job-pvc-name-exist"
		taskName := "pvctask"
		pvName := "job-pv-name"
		pvcName := "job-pvc-name-exist"
		ctx := initTestContext(options{})
		defer cleanupTestContext(ctx)

		var tt v12.HostPathType = "DirectoryOrCreate"

		storageClsName := "standard"

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
		_, err := ctx.kubeclient.CoreV1().PersistentVolumes().Create(context.TODO(), &pv, metav1.CreateOptions{})
		Expect(err).NotTo(HaveOccurred(), "pv creation ")
		// create pvc
		pvc := v12.PersistentVolumeClaim{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: ctx.namespace,
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

		_, err1 := ctx.kubeclient.CoreV1().PersistentVolumeClaims(ctx.namespace).Create(context.TODO(), &pvc, metav1.CreateOptions{})
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
		job := createJob(ctx, &jobSpec{
			namespace: ctx.namespace,
			name:      jobName,
			tasks: []taskSpec{
				{
					img:  defaultNginxImage,
					req:  oneCPU,
					min:  1,
					rep:  1,
					name: taskName,
				},
			},
			volumes: []v1alpha1.VolumeSpec{
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

		err = waitJobReady(ctx, job)
		Expect(err).NotTo(HaveOccurred())

		job, err = ctx.vcclient.BatchV1alpha1().Jobs(ctx.namespace).Get(context.TODO(), jobName, metav1.GetOptions{})
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
		ctx := initTestContext(options{})
		defer cleanupTestContext(ctx)

		resource := v12.ResourceList{
			"cpu":            resource.MustParse("1000m"),
			"memory":         resource.MustParse("1000Mi"),
			"nvidia.com/gpu": resource.MustParse("1"),
		}

		job := createJob(ctx, &jobSpec{
			namespace: ctx.namespace,
			name:      jobName,
			tasks: []taskSpec{
				{
					img:   defaultNginxImage,
					min:   1,
					rep:   1,
					name:  "task-1",
					req:   resource,
					limit: resource,
				},
				{
					img:   defaultNginxImage,
					min:   1,
					rep:   1,
					name:  "task-2",
					req:   resource,
					limit: resource,
				},
			},
		})

		expected := map[string]int64{
			"cpu":            2,
			"memory":         1024 * 1024 * 2000,
			"nvidia.com/gpu": 2,
		}

		err := waitJobStatePending(ctx, job)
		Expect(err).NotTo(HaveOccurred())

		pGroup, err := ctx.vcclient.SchedulingV1beta1().PodGroups(ctx.namespace).Get(context.TODO(), jobName, metav1.GetOptions{})
		Expect(err).NotTo(HaveOccurred())

		for name, q := range *pGroup.Spec.MinResources {
			value, ok := expected[string(name)]
			Expect(ok).To(Equal(true), "Resource %s should exists in PodGroup", name)
			Expect(q.Value()).To(Equal(value), "Resource %s 's value should equal to %d", name, value)
		}

	})
})
