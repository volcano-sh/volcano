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

package util

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNewFakeBinder(t *testing.T) {
	tests := []struct {
		fkBinder    *FakeBinder
		cap, length int
	}{
		{
			fkBinder: NewFakeBinder(0),
			cap:      0, length: 0,
		},
		{
			fkBinder: NewFakeBinder(10),
			cap:      10, length: 0,
		},
	}
	for _, test := range tests {
		assert.Equal(t, test.cap, cap(test.fkBinder.Channel))
		assert.Equal(t, test.length, test.fkBinder.Length())
		assert.Equal(t, test.length, len(test.fkBinder.Binds()))
	}
}

func TestNewFakeEvictor(t *testing.T) {
	tests := []struct {
		fkEvictor   *FakeEvictor
		cap, length int
	}{
		{
			fkEvictor: NewFakeEvictor(0),
			cap:       0, length: 0,
		},
		{
			fkEvictor: NewFakeEvictor(10),
			cap:       10, length: 0,
		},
	}
	for _, test := range tests {
		assert.Equal(t, test.cap, cap(test.fkEvictor.Channel))
		assert.Equal(t, test.cap, cap(test.fkEvictor.evicts))
		assert.Equal(t, test.length, test.fkEvictor.Length())
		assert.Equal(t, test.length, len(test.fkEvictor.Evicts()))
	}
}
