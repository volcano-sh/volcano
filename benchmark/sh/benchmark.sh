#!/bin/bash
source base.sh
set -e

aff=false
while getopts ":a:" opt; do
  case $opt in
    a)
      aff=true
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

group=${2:-$1}

start_wait_sleep=150
end_wait_sleep=80

# 校验参数，如果group为空，直接退出脚本
if [ -z "$group" ]; then
    echo "group is empty, please check!"
    exit 1
fi

if [ "$group" = "1" ]; then
  deployment_count=1
  replicas_count=5000
  if [ "$aff" = true ]; then
    start_wait_sleep=200
    end_wait_sleep=100
  else
    start_wait_sleep=150
    end_wait_sleep=80
  fi
elif [ "$group" = "2" ]; then
  deployment_count=5
  replicas_count=1000
  if [ "$aff" = true ]; then
    start_wait_sleep=200
    end_wait_sleep=150
  else
    start_wait_sleep=250
    end_wait_sleep=150
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
else
  deployment_count=4
  replicas_count=2
  start_wait_sleep=30
  end_wait_sleep=15
fi

echo "group $group: deployment $deployment_count replicas $replicas_count"

echo "use default-scheduler test start"
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
echo "use default-scheduler test end"

echo "use volcano test start"
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
echo "use volcano test end"

echo "time range: $start_ts_default~$end_ts_default $start_ts_volcano~$end_ts_volcano"
./query.sh "$group" "$start_ts_default" "$end_ts_default" "$start_ts_volcano" "$end_ts_volcano" "$aff"