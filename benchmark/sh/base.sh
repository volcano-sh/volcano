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