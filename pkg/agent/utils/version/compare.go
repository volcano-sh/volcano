/*
Copyright 2024 The Volcano Authors.

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

package version

import (
	"regexp"
	"strconv"
	"strings"
)

// HigherOrEqual return whether version1 >= version2.
// example:
// 23.03 (LTS-SP2) > 22.03 (LTS-SP2) => true
// 21.04 (LTS-SP2) < 22.03 (LTS-SP2) => false
// 22.03 (LTS-SP1) < 22.03 (LTS-SP2) => false
// 22.03 (LTS-SP2) = 22.03 (LTS-SP2) => true
func HigherOrEqual(version1, version2 string) bool {
	v1 := strings.Split(version1, " ")
	if len(v1) <= 1 {
		return false
	}
	v2 := strings.Split(version2, " ")
	if len(v2) <= 1 {
		return false
	}
	return compareVersion(v1[0], v2[0]) > 0 || (compareVersion(v1[0], v2[0]) == 0) && compareSP(v1[1], v2[1]) >= 0
}

func compareVersion(version1, version2 string) int {
	v1 := strings.Split(version1, ".")
	v2 := strings.Split(version2, ".")
	var err error
	for i := 0; i < len(v1) || i < len(v2); i++ {
		x, y := 0, 0
		if i < len(v1) {
			x, err = strconv.Atoi(v1[i])
			if err != nil {
				return -1
			}
		}
		if i < len(v2) {
			y, err = strconv.Atoi(v2[i])
			if err != nil {
				return -1
			}
		}
		if x > y {
			return 1
		}
		if x < y {
			return -1
		}
	}
	return 0
}

func compareSP(s1, s2 string) int {
	re := regexp.MustCompile(`SP(\d)\)`)
	sub1 := re.FindStringSubmatch(s1)
	if len(sub1) <= 1 {
		return -1
	}
	sub2 := re.FindStringSubmatch(s2)
	if len(sub2) <= 1 {
		return -1
	}
	return strings.Compare(sub1[1], sub2[1])
}
