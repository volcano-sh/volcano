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

package uthelper

import (
	"volcano.sh/volcano/pkg/scheduler/conf"
	"volcano.sh/volcano/pkg/scheduler/framework"
)

// Interface is UT framework interface
type Interface interface {
	// Run executes the actions
	Run(actions []framework.Action)
	// RegistSession init the session
	RegistSession(tiers []conf.Tier, config []conf.Configuration) *framework.Session
	// Close release session and do cleanup
	Close()
	// CheckAll do all checks
	CheckAll(caseIndex int) (err error)
	// CheckBind just check bind results in allocate action
	CheckBind(caseIndex int) error
	// CheckEvict just check evict results in preempt or reclaim action
	CheckEvict(caseIndex int) error
	// CheckPipelined check the pipelined results
	CheckPipelined(caseIndex int) error
	// CheckPGStatus check job's status
	CheckPGStatus(caseIndex int) error
}
