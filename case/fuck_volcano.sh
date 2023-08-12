#!/bin/bash

podUID=""
pgName=""
queueName=""

## get object
get_object() {
    podName=$1
    ns=$2
    podUID=$(kubectl get pod $podName -n $2 -o jsonpath='{.metadata.uid}')
    echo "taskid:"$podUID

    pgName=$(kubectl get pod $podName -n $2 -o jsonpath='{.metadata.annotations.scheduling\.k8s\.io/group-name}')
    echo "pg:"$pgName

    queueName=$(kubectl get podgroup $pgName -n $2 -o jsonpath='{.spec.queue}')
    echo "queue:"$queueName
    echo "###################"
}

get_object $1 $2

get_one_session() {
  tac v.log | awk '/Close Session/, /Open Session/'| awk '{print} /Open Session/ {exit}' > output.txt
  tac output.txt >  output2.txt
}

check_enqueue() {
    q=$1
    filename=$2
    grep 'Reject job enqueue, the queue resource quota limit has reached after add job' $filename
    awk -v qq="$q" '/queue <'"$q"'> is meet/ { print; getline; if ($0 ~ "The attributes of queue <'"${q//\"}"'>") print }' $filename
    echo "###################"
}

check_allocate() {
    v1=$1
    v2=$2
    filename=$3
    grep -P "Try to allocate resource to \d+ tasks of Job <$v1/$v2>" $filename
    grep -F "Failed to bind Task $podUID" $filename
    echo "###################"
}


get_one_session
check_enqueue $queueName output2.txt
check_allocate $2 $pgName  output2.txt