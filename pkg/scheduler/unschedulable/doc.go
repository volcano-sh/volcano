/*
Copyright 2026 The Volcano Authors.

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

// Package unschedulable contains the cache and plugin contracts used to avoid
// repeating work for Jobs rejected in a previous scheduling session.
//
// A Rejection records why a plugin rejected a Job or task. A Hint evaluates
// whether a cluster event can invalidate that rejection. ComputeSkip converts
// reusable rejections into the api.SkipDecision consumed by scheduler actions.
// HintSkip keeps a Job cached; HintWakeup removes it for reevaluation.
package unschedulable
