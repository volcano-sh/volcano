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

package utils

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSizeStrConvertToByteSize(t *testing.T) {
	testCases := []struct {
		name    string
		sizeStr string

		expectedSize  uint64
		expectedError bool
	}{

		{
			name:          "Kbps",
			sizeStr:       "10Kbps",
			expectedSize:  10 * 1000 / 8,
			expectedError: false,
		},
		{
			name:          "Kibps",
			sizeStr:       "100Kibps",
			expectedSize:  100 * 1024 / 8,
			expectedError: false,
		},

		{
			name:          "Mbps",
			sizeStr:       "100Mbps",
			expectedSize:  100 * 1000 * 1000 / 8,
			expectedError: false,
		},
		{
			name:          "Mibps",
			sizeStr:       "1000Mibps",
			expectedSize:  1000 * 1024 * 1024 / 8,
			expectedError: false,
		},

		{
			name:          "Gbps",
			sizeStr:       "20Gbps",
			expectedSize:  20 * 1000 * 1000 * 1000 / 8,
			expectedError: false,
		},
		{
			name:          "Gibps",
			sizeStr:       "200Gibps",
			expectedSize:  200 * 1024 * 1024 * 1024 / 8,
			expectedError: false,
		},

		{
			name:          "Tbps",
			sizeStr:       "20Tbps",
			expectedSize:  20 * 1000 * 1000 * 1000 * 1000 / 8,
			expectedError: false,
		},

		{
			name:          "Tibps",
			sizeStr:       "200Tibps",
			expectedSize:  200 * 1024 * 1024 * 1024 * 1024 / 8,
			expectedError: false,
		},

		{
			name:          "illegal unit",
			sizeStr:       "100000FAKE",
			expectedSize:  0,
			expectedError: true,
		},

		{
			name:          "illegal numbers",
			sizeStr:       "100000.23Mbps",
			expectedSize:  0,
			expectedError: true,
		},
	}
	for _, tc := range testCases {
		actualSize, actualErr := SizeStrConvertToByteSize(tc.sizeStr)
		assert.Equal(t, tc.expectedSize, actualSize, tc.name)
		assert.Equal(t, tc.expectedError, actualErr != nil, tc.name)
	}
}
