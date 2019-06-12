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

package helpers

import (
	"fmt"
	"k8s.io/api/core/v1"
	"math/rand"
	"strings"
	"time"
)

const (
	// PodNameFmt pod name format
	PodNameFmt = "%s-%s-%d"
	// VolumeClaimFmt  volume claim name format
	VolumeClaimFmt = "%s-volume-%s"
)

// GetTaskIndex   returns task Index
func GetTaskIndex(pod *v1.Pod) string {
	num := strings.Split(pod.Name, "-")
	if len(num) >= 3 {
		return num[len(num)-1]
	}

	return ""
}

// MakePodName creates pod name
func MakePodName(jobName string, taskName string, index int) string {
	return fmt.Sprintf(PodNameFmt, jobName, taskName, index)
}

func genRandomStr(l int) string {
	str := "0123456789abcdefghijklmnopqrstuvwxyz"
	bytes := []byte(str)
	var result []byte
	r := rand.New(rand.NewSource(time.Now().UnixNano()))
	for i := 0; i < l; i++ {
		result = append(result, bytes[r.Intn(len(bytes))])
	}
	return string(result)
}

// MakeVolumeClaimName creates volume claim name
func MakeVolumeClaimName(jobName string) string {
	return fmt.Sprintf(VolumeClaimFmt, jobName, genRandomStr(12))
}
