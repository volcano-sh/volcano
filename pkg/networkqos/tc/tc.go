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

package tc

import (
	"sync"
	"time"
)

const (
	CmdTimeout = 5 * time.Second
)

//go:generate mockgen -destination ./mocks/mock_tc.go -package mocks -source tc.go

// just support linux, related bug: https://github.com/vishvananda/netns/issues/23

// TC defines traffic control operations
type TC interface {
	PreAddFilter(netns string, ifName string) (bool, error)
	AddFilter(netns string, ifName string) error
	RemoveFilter(netns string, ifName string) error
}

// TCCmd is the concrete implementation of TC
type TCCmd struct{}

// Compile-time check to ensure TCCmd implements TC
var _ TC = (*TCCmd)(nil)

// singleton instance
var (
	tcCmd TC
	once  sync.Once
)

// GetTCCmd returns the singleton TC implementation
func GetTCCmd() TC {
	once.Do(func() {
		tcCmd = &TCCmd{}
	})
	return tcCmd
}

// SetTcCmd allows overriding the TC implementation (used in tests)
func SetTcCmd(tc TC) {
	tcCmd = tc
}

//
// ---- TCCmd method implementations are platform-specific (tc_linux.go or tc_unspecified.go) ----
//
