/*
Copyright 2025 The Volcano Authors.

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

package vgpu

import (
    "testing"

    "volcano.sh/volcano/pkg/scheduler/api/devices/config"
)

func TestFindMatch(t *testing.T) {
    testCases := []struct {
        name              string
        uuid              string
        requestMem        uint
        usage             config.MigInUse
        allowedGeometries []config.Geometry
        wantFit           bool
        wantUUID          string
        wantMem           uint
    }{
        {
            name:       "Match 1g.10gb and available",
            uuid:       "gpu-1234",
            requestMem: 10,
            usage: config.MigInUse{
                Index: 0,
                UsageList: config.MIGS{
                    {
                        Name:      "1g.10gb",
                        Memory:    10240,
                        InUse:     false,
                        UsedIndex: []int{},
                    },
                },
            },
            allowedGeometries: []config.Geometry{
                {
                    Group: "default",
                    Instances: []config.MigTemplate{
                        {
                            Name:   "1g.10gb",
                            Memory: 10240,
                            Count:  2,
                        },
                    },
                },
            },
            wantFit:  true,
            wantUUID: "gpu-1234[default-0]",
            wantMem:  10240,
        },
        {
            name:       "No match due to memory too high",
            uuid:       "gpu-1234",
            requestMem: 20000,
            usage: config.MigInUse{
                Index: 0,
                UsageList: config.MIGS{
                    {
                        Name:      "1g.10gb",
                        Memory:    10,
                        InUse:     false,
                        UsedIndex: []int{},
                    },
                },
            },
            allowedGeometries: []config.Geometry{
                {
                    Group: "default",
                    Instances: []config.MigTemplate{
                        {
                            Name:   "1g.10gb",
                            Memory: 10,
                            Count:  1,
                        },
                    },
                },
            },
            wantFit:  false,
            wantUUID: "",
            wantMem:  0,
        },
        {
            name:       "Empty geometry list",
            uuid:       "gpu-1234",
            requestMem: 10,
            usage: config.MigInUse{
                Index:     0,
                UsageList: config.MIGS{},
            },
            allowedGeometries: []config.Geometry{
                {},
            },
            wantFit:  false,
            wantUUID: "",
            wantMem:  0,
        },
        {
            name:       "No matching MIG template in geometry",
            uuid:       "gpu-0002",
            requestMem: 20,
            usage: config.MigInUse{
                Index: 0,
                UsageList: config.MIGS{
                    {
                        Name:      "1g.10gb",
                        Memory:    10,
                        InUse:     false,
                        UsedIndex: []int{},
                    },
                },
            },
            allowedGeometries: []config.Geometry{
                {
                    Group: "default",
                    Instances: []config.MigTemplate{
                        {
                            Name:   "2g.20gb",
                            Memory: 20,
                            Count:  1,
                        },
                    },
                },
            },
            wantFit:  false,
            wantUUID: "",
            wantMem:  0,
        },
        {
            name:       "MIG instance already in use",
            uuid:       "gpu-1234",
            requestMem: 10,
            usage: config.MigInUse{
                Index: 0,
                UsageList: config.MIGS{
                    {
                        Name:      "1g.10gb",
                        Memory:    10,
                        InUse:     true,
                        UsedIndex: []int{0},
                    },
                },
            },
            allowedGeometries: []config.Geometry{
                {
                    Group: "default",
                    Instances: []config.MigTemplate{
                        {
                            Name:   "1g.10gb",
                            Memory: 10,
                            Count:  1,
                        },
                    },
                },
            },
            wantFit:  false,
            wantUUID: "",
            wantMem:  0,
        },
        {
            name:       "Multiple templates, pick first matching and available",
            uuid:       "gpu-1234",
            requestMem: 20,
            usage: config.MigInUse{
                Index:     -1,
                UsageList: config.MIGS{},
            },
            allowedGeometries: []config.Geometry{
                {
                    Group: "group1",
                    Instances: []config.MigTemplate{
                        {
                            Name:   "1g.10gb",
                            Memory: 10,
                            Count:  1,
                        },
                    },
                },
                {
                    Group: "group2",
                    Instances: []config.MigTemplate{
                        {
                            Name:   "2g.20gb",
                            Memory: 24,
                            Count:  1,
                        },
                    },
                },
            },
            wantFit:  true,
            wantUUID: "gpu-1234[group2-0]",
            wantMem:  24,
        },
        {
            name:       "Request memory smaller than template, still match",
            uuid:       "gpu-0005",
            requestMem: 5,
            usage: config.MigInUse{
                Index: 0,
                UsageList: config.MIGS{
                    {
                        Name:      "1g.10gb",
                        Memory:    10,
                        InUse:     false,
                        UsedIndex: []int{},
                    },
                },
            },
            allowedGeometries: []config.Geometry{
                {
                    Group: "default",
                    Instances: []config.MigTemplate{
                        {Name: "1g.10gb", Memory: 10, Count: 1},
                    },
                },
            },
            wantFit:  true,
            wantUUID: "gpu-0005[default-0]",
            wantMem:  10,
        },
    }

    for _, tc := range testCases {
        t.Run(tc.name, func(t *testing.T) {
            gotFit, gotUUID, gotMem := findMatch(tc.uuid, tc.requestMem, tc.usage, tc.allowedGeometries)

            if gotFit != tc.wantFit {
                t.Errorf("findMatch() gotFit = %v, want %v", gotFit, tc.wantFit)
            }
            if gotUUID != tc.wantUUID {
                t.Errorf("findMatch() gotUUID = %v, want %v", gotUUID, tc.wantUUID)
            }
            if gotMem != tc.wantMem {
                t.Errorf("findMatch() gotMem = %v, want %v", gotMem, tc.wantMem)
            }
        })
    }
}
