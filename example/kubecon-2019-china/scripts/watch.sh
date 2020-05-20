#!/bin/bash

watch_cluster() {
    echo "Nodes:"
    node-info.exe

    echo ""
    echo ""

    echo "Pods:"
    echo "-------------------------------"
    kubectl get pods

    echo ""
    echo ""

    echo "Volcano Jobs:"
    echo "-------------------------------"
    vcctl job list
}

if [ $# == 0 ]; then
    watch_cluster
else

    while getopts "f" arg
    do
        case $arg in
            "f")
                while [ 1 ]
                do
                    clear
                    watch_cluster
                    sleep 3
                done
                ;;
             ?)
                echo "Unknown arguments"
        esac
    done
fi