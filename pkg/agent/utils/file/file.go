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

package file

import (
	"os"
	"strconv"
	"strings"

	"k8s.io/klog/v2"
)

func ReadIntFromFile(file string) (int64, error) {
	data, err := os.ReadFile(file)
	if err != nil {
		return 0, err
	}
	value, err := strconv.Atoi(strings.TrimRight(string(data), "\n"))
	if err != nil {
		return 0, err
	}
	return int64(value), nil
}

func ReadByteFromFile(file string) ([]byte, error) {
	return os.ReadFile(file)
}

// ReadBatchFromFile read files and return file name mapping value.
func ReadBatchFromFile(files []string) map[string]string {
	res := make(map[string]string)
	for _, file := range files {
		val, err := os.ReadFile(file)
		if err != nil {
			klog.ErrorS(err, "Failed to read file", "name", file)
			continue
		}
		res[file] = string(val)
	}
	return res
}

func WriteByteToFile(file string, content []byte) error {
	return os.WriteFile(file, content, 0644)
}
