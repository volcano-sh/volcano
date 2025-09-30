#!/bin/sh

# Copyright 2024 The Volcano Authors.

# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at

#     http://www.apache.org/licenses/LICENSE-2.0

# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

set -e

VOLCANO_AGENT_LOG_DIR="/var/log/volcano/agent"
VOLCANO_AGENT_LOG_PATH="${VOLCANO_AGENT_LOG_DIR}/volcano-agent.log"
NETWORK_QOS_LOG_PATH="${VOLCANO_AGENT_LOG_DIR}/network-qos.log"
NETWORK_QOS_TOOLS_LOG_PATH="${VOLCANO_AGENT_LOG_DIR}/network-qos-tools.log"

MEMORY_QOS_ENABLED_PATH="/host/proc/sys/vm/memcg_qos_enable"
LOAD_BALANCE_ENABLED_PATH="/host/proc/sys/kernel/sched_prio_load_balance_enabled"

function log()
{
    echo "[`date "+%Y-%m-%d %H:%M:%S"`] $*"
}

function set_sched_prio_load_balance_enabled() {
    log "Start to enable cpu load balance"
    if [[ ! -f ${LOAD_BALANCE_ENABLED_PATH} ]]; then
        log "Skip enabling CPU load balance: file(${LOAD_BALANCE_ENABLED_PATH}) not found. OverSubscription, Node Pressure Eviction colocation features remain available"
        return 0
    fi
    cat ${LOAD_BALANCE_ENABLED_PATH}
    echo 1 > ${LOAD_BALANCE_ENABLED_PATH}
    log "Successfully enabled cpu load balance"
}

function set_memory_qos_enabled(){
    log "Start to enable memory qos enable"
    if [[ ! -f ${MEMORY_QOS_ENABLED_PATH} ]]; then
        log "Skip enabling memory cgroup qos: file(${MEMORY_QOS_ENABLED_PATH}) not found. OverSubscription, Node Pressure Eviction colocation features remain available"
        return 0
    fi
    cat ${MEMORY_QOS_ENABLED_PATH}
    echo 1 > ${MEMORY_QOS_ENABLED_PATH}
    log "Successfully enabled memory qos"
}

touch ${VOLCANO_AGENT_LOG_PATH}
touch ${NETWORK_QOS_LOG_PATH}
touch ${NETWORK_QOS_TOOLS_LOG_PATH}

chmod 750 ${VOLCANO_AGENT_LOG_DIR}
chown -R 1000:1000 ${VOLCANO_AGENT_LOG_DIR}
chmod 640 ${VOLCANO_AGENT_LOG_DIR}/*.log

set_memory_qos_enabled
set_sched_prio_load_balance_enabled
