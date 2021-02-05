/*
Copyright 2019 The Kubernetes Authors.

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

package actions

import (
	"volcano.sh/volcano/pkg/scheduler/actions/allocate"
	"volcano.sh/volcano/pkg/scheduler/actions/backfill"
	"volcano.sh/volcano/pkg/scheduler/actions/elect"
	"volcano.sh/volcano/pkg/scheduler/actions/enqueue"
	"volcano.sh/volcano/pkg/scheduler/actions/preempt"
	"volcano.sh/volcano/pkg/scheduler/actions/reclaim"
	"volcano.sh/volcano/pkg/scheduler/actions/reserve"
	"volcano.sh/volcano/pkg/scheduler/framework"
)

func init() {
	framework.RegisterAction(reclaim.New())
	framework.RegisterAction(allocate.New())
	framework.RegisterAction(backfill.New())
	framework.RegisterAction(preempt.New())
	framework.RegisterAction(enqueue.New())
	framework.RegisterAction(elect.New())
	framework.RegisterAction(reserve.New())
}
