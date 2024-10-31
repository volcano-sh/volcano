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
source base.sh
set -e

# 初始化变量
aff=false  # 是否测试亲和性
mini=false # 是否启用mini volcano

# 解析参数 -a 和 -m
while getopts ":am" opt; do
  case $opt in
    a)
      aff=true
      ;;
    m)
      mini=true
      ;;
    \?)
      show_help
      exit 1
      ;;
    :)
      show_help
      exit 1
      ;;
  esac
done

# 处理 positional parameters，group 参数
shift $((OPTIND-1))  # 移除已处理的选项
group=${1:-""}  # 如果 positional parameter 为空，group 将为空字符串

# 校验参数，如果 group 为空，直接退出脚本
if [ -z "$group" ]; then
    echo "group is empty, please check!"
    exit 1
fi

start_wait_sleep=150
end_wait_sleep=80

if [ "$group" = "1" ]; then
  deployment_count=1
  replicas_count=5000
  if [ "$aff" = true ]; then
    start_wait_sleep=180
    end_wait_sleep=100
  else
    start_wait_sleep=80
    end_wait_sleep=50
  fi
elif [ "$group" = "2" ]; then
  deployment_count=5
  replicas_count=1000
  if [ "$aff" = true ]; then
    start_wait_sleep=200
    end_wait_sleep=150
  else
    start_wait_sleep=220
    end_wait_sleep=120
  fi
elif [ "$group" = "3" ]; then
  deployment_count=25
  replicas_count=200
  if [ "$aff" = true ]; then
    start_wait_sleep=200
    end_wait_sleep=150
  else
    start_wait_sleep=220
    end_wait_sleep=150
  fi
elif [ "$group" = "4" ]; then
  deployment_count=100
  replicas_count=50
  if [ "$aff" = true ]; then
    start_wait_sleep=200
    end_wait_sleep=150
  else
    start_wait_sleep=220
    end_wait_sleep=150
  fi
elif [ "$group" = "5" ]; then
  deployment_count=200
  replicas_count=25
  if [ "$aff" = true ]; then
    start_wait_sleep=200
    end_wait_sleep=150
  else
    start_wait_sleep=220
    end_wait_sleep=150
  fi
elif [ "$group" = "6" ]; then
  deployment_count=500
  replicas_count=10
  if [ "$aff" = true ]; then
    start_wait_sleep=200
    end_wait_sleep=150
  else
    start_wait_sleep=220
    end_wait_sleep=150
  fi
else
  deployment_count=4
  replicas_count=2
  start_wait_sleep=6
  end_wait_sleep=3
fi

echo "group $group: deployment $deployment_count replicas $replicas_count"

echo "====== use default-scheduler test start ======"
start_ts_default=$(date +%s)
if [ "$aff" = true ]; then
  ./kwok/deploy-tool.sh -a -i 0.05 $deployment_count $replicas_count
else
  ./kwok/deploy-tool.sh -i 0.05 $deployment_count $replicas_count
fi
sleep_with_progress $start_wait_sleep
end_ts_default=$(date +%s)
./kwok/deploy-tool.sh -d -i 0.05 $deployment_count $replicas_count
sleep_with_progress $end_wait_sleep
echo "====== use default-scheduler test end ======"

echo "====== use volcano test start ======"
start_ts_volcano=$(date +%s)
if [ "$aff" = true ]; then
  ./kwok/deploy-tool.sh -v -a -i 0.05 $deployment_count $replicas_count
else
  ./kwok/deploy-tool.sh -v -i 0.05 $deployment_count $replicas_count
fi
sleep_with_progress $start_wait_sleep
end_ts_volcano=$(date +%s)
./kwok/deploy-tool.sh -d -v -i 0.05 $deployment_count $replicas_count
sleep_with_progress $end_wait_sleep
echo "====== use volcano test end ======"

echo "time range: $start_ts_default~$end_ts_default $start_ts_volcano~$end_ts_volcano"
./query.sh "$group" "$start_ts_default" "$end_ts_default" "$start_ts_volcano" "$end_ts_volcano" "$aff" "$mini"