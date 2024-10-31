#!/bin/bash
# Copyright 2019 The Volcano Authors.
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

set -e

if [ $# -eq 0 ]; then
  echo "Error: Please provide the number of nodes to uninstall."
  echo "Usage: $0 <number_of_nodes>"
  exit 1
fi

for (( i=0;i<$1; i++))
do
  kubectl delete node kwok-node-"$i"
done