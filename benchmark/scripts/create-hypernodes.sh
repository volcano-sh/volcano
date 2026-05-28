#!/usr/bin/env bash
# create-hypernodes.sh — Create HyperNode CRs for topology-aware scheduling
#
# MUST be run AFTER Volcano is installed (the HyperNode CRD comes from Volcano).
#
# Usage:
#   ./scripts/create-hypernodes.sh                             # 4 racks / 2 spines (defaults)
#   TOPOLOGY_RACKS=8 TOPOLOGY_SPINES=4 ./scripts/create-hypernodes.sh
#
# Environment variables:
#   TOPOLOGY_RACKS    - Number of rack-level domains (tier 1). Default: 4
#   TOPOLOGY_SPINES   - Number of spine-level domains (tier 2). Default: 2
#
# Topology layout (TOPOLOGY_RACKS=4, TOPOLOGY_SPINES=2):
#   spine-0 (tier 2)
#   ├── rack-0 (tier 1) — selects nodes with label topology-rack=rack-0
#   ├── rack-1 (tier 1) — selects nodes with label topology-rack=rack-1
#   spine-1 (tier 2)
#   ├── rack-2 (tier 1) — selects nodes with label topology-rack=rack-2
#   └── rack-3 (tier 1) — selects nodes with label topology-rack=rack-3
#
# Prerequisites:
#   - Volcano installed (so that topology.volcano.sh CRD exists)
#   - KWOK nodes created with topology labels (via create-kwok-nodes.sh ENABLE_TOPOLOGY=true)

source "$(dirname "$0")/common.sh"
require_cmd kubectl

# Verify HyperNode CRD exists
function verify_crd() {
    if ! kubectl get crd hypernodes.topology.volcano.sh >/dev/null 2>&1; then
        log_error "HyperNode CRD (hypernodes.topology.volcano.sh) not found."
        log_error "Please install Volcano first (make install-volcano) before creating HyperNodes."
        exit 1
    fi
}

# Create HyperNode CRs for topology-aware scheduling
function create_hypernode_topology() {
    local racks_per_spine=$(( TOPOLOGY_RACKS / TOPOLOGY_SPINES ))

    log_info "Creating HyperNode topology: ${TOPOLOGY_RACKS} rack HyperNodes + ${TOPOLOGY_SPINES} spine HyperNodes"

    # Create tier-1 HyperNodes (racks) — each groups nodes by label
    for spine in $(seq 0 $((TOPOLOGY_SPINES - 1))); do
        for rack_in_spine in $(seq 0 $((racks_per_spine - 1))); do
            local rack_idx=$(( spine * racks_per_spine + rack_in_spine ))
            cat <<EOF | kubectl apply -f -
apiVersion: topology.volcano.sh/v1alpha1
kind: HyperNode
metadata:
  name: rack-${rack_idx}
spec:
  tier: 1
  tierName: rack
  members:
    - type: Node
      selector:
        labelMatch:
          matchLabels:
            type: kwok
            topology-rack: "rack-${rack_idx}"
EOF
        done
    done

    # Create tier-2 HyperNodes (spines) — each groups racks
    for spine in $(seq 0 $((TOPOLOGY_SPINES - 1))); do
        local members=""
        for rack_in_spine in $(seq 0 $((racks_per_spine - 1))); do
            local rack_idx=$(( spine * racks_per_spine + rack_in_spine ))
            members="${members}
    - type: HyperNode
      selector:
        exactMatch:
          name: rack-${rack_idx}"
        done
        cat <<EOF | kubectl apply -f -
apiVersion: topology.volcano.sh/v1alpha1
kind: HyperNode
metadata:
  name: spine-${spine}
spec:
  tier: 2
  tierName: spine
  members:${members}
EOF
    done

    log_info "HyperNode topology creation complete"
}

# Main
verify_crd
create_hypernode_topology
