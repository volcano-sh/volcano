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

group=$1
# 两个时间段的开始和结束时间（Unix 时间戳）
start_ts_default=$2  # 第一个时间段的开始时间
end_ts_default=$3    # 第一个时间段的结束时间
start_ts_volcano=$4  # 第二个时间段的开始时间
end_ts_volcano=$5    # 第二个时间段的结束时间
aff=$6
use_mini=$7 # 是否使用mini volcano

# 校验参数，如果group为空，直接退出脚本
if [ -z "$group" ]; then
    echo "group is empty, please check!"
    exit 1
fi

# 校验时间参数个数，必须为7个
if [ $# -ne 7 ]; then
  echo "must have group,start_ts_default,end_ts_default,start_ts_volcano,end_ts_volcano,aff,use_mini"
  exit 1
fi

kubectl port-forward svc/kube-prometheus-stack-prometheus 9090:9090 -n monitoring &
PORT_FORWARD_PID=$!

echo "PORT_FORWARD_PID: $PORT_FORWARD_PID"

# 定义清理函数，用于关闭 port-forward 进程
cleanup() {
  echo "清理操作：关闭 port-forward 进程 (PID: $PORT_FORWARD_PID)..."
  kill $PORT_FORWARD_PID
  echo "清理操作：删除临时文件data.txt"
  rm -rf data.txt
}

# 使用 trap 捕捉 EXIT 信号，确保无论脚本如何退出都能执行 cleanup
trap cleanup EXIT

# 等待Prometheus端口代理到本地
sleep 3

# 配置 Prometheus 的 URL 和查询
PROMETHEUS_URL="http://localhost:9090/api/v1/query_range"
QUERY="count(kube_pod_container_status_ready\{namespace=%22default%22\})"

# 时间步长（每60秒采样一次）
STEP=1s

echo "$PROMETHEUS_URL?query=$QUERY&start=$start_ts_default&end=$end_ts_default&step=$STEP"
echo "$PROMETHEUS_URL?query=$QUERY&start=$start_ts_volcano&end=$end_ts_volcano&step=$STEP"

# 获取第一个时间段的数据
DATA1=$(curl -s "$PROMETHEUS_URL?query=$QUERY&start=$start_ts_default&end=$end_ts_default&step=$STEP" | jq '.data.result[0].values')

# 获取第二个时间段的数据
DATA2=$(curl -s "$PROMETHEUS_URL?query=$QUERY&start=$start_ts_volcano&end=$end_ts_volcano&step=$STEP" | jq '.data.result[0].values')

# 提取时间和count值，将两个时间段的起点对齐
start_ts_default_ALIGNED=$start_ts_default
start_ts_volcano_ALIGNED=$start_ts_default  # 将第二个时间段的起点对齐到第一个时间段

# 处理数据，将数据保存到文件
echo "# Time (aligned)  Count1  Count2" > data.txt
for (( i=0; i<$(echo $DATA1 | jq 'length'); i++ )); do
  TIME1=$(echo $DATA1 | jq -r ".[$i][0]")
  COUNT1=$(echo $DATA1 | jq -r ".[$i][1]")
  TIME2=$(echo $DATA2 | jq -r ".[$i][0]")
  COUNT2=$(echo $DATA2 | jq -r ".[$i][1]")

  # 对齐时间到 start_ts_default
  ALIGNED_TIME1=$((TIME1 - start_ts_default_ALIGNED))
  ALIGNED_TIME2=$((TIME2 - start_ts_volcano_ALIGNED))

  # 将对齐后的时间和 count 值写入文件
  echo "$ALIGNED_TIME1  $COUNT1  $COUNT2" >> data.txt
done

if [ "$aff" = true ]; then
  aff_prefix="_aff"
  img_name="g$group$aff_prefix"
else
  img_name="g$group"
fi

if [ "$use_mini" = true ]; then
  img_name="$img_name-mini"
fi

# 使用 gnuplot 绘制对比图
gnuplot <<EOF
set terminal pngcairo size 800,600
set output '../img/res/$img_name.png'
set title "Group $group"
set xlabel "Time (seconds from start)"
set ylabel "Ready Pod Count"
set grid

set yrange [0:5100]  # 自动扩展 Y 轴，但从 0 开始
plot "data.txt" using 1:2 with lines title "Default", \
     "data.txt" using 1:3 with lines title "Volcano"
EOF

echo "benchmark对比图已生成：$img_name.png"