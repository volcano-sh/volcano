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
	"time"
)

const (
	CmdTimeout = 5 * time.Second
)

//go:generate mockgen -destination  ./mocks/mock_tc.go -package mocks -source tc.go

// just support linux, related bug: https://github.com/vishvananda/netns/issues/23

type TC interface {
	PreAddFilter(netns, ifName string) (bool, error)
	AddFilter(netns, ifName string) error
	RemoveFilter(netns, ifName string) error
}

var _ TC = &TCCmd{}

type TCCmd struct{}

var tcCmd TC

func GetTCCmd() TC {
	if tcCmd == nil {
		tcCmd = &TCCmd{}
	}
	return tcCmd
}

func SetTcCmd(tc TC) {
	tcCmd = tc
}
