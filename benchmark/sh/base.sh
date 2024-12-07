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

# 在当前进程sleep xxx s，并显示实时进度条
sleep_with_progress() {
    duration=$1
    for ((i = 0; i <= duration; i++)); do
        percent=$((i * 100 / duration))
        bar=$(printf '%0.s=' $(seq 1 $((percent / 2))))
        echo -ne "Progress: [${bar}] ${percent}%\r"
        sleep 1
    done
    echo -ne "\n"
}

# 检查镜像是否存在，不存在则拉取，然后加载到kind集群
load_image_to_kind() {
  local image_name="$1"   # 镜像名，包括repo和tag
  local kind_cluster="$2" # kind集群名称

  # 使用docker检查镜像是否存在
  if ! docker images "$image_name" --format "{{.Repository}}:{{.Tag}}" | grep -q "$image_name"; then
    echo "Pulling image: $image_name"
    docker pull "$image_name"
  else
    echo "Image $image_name already exists locally."
  fi

  # 将镜像load到指定的kind集群
  echo "Loading image $image_name into kind cluster $kind_cluster"
  kind load docker-image "$image_name" --name "$kind_cluster"
}