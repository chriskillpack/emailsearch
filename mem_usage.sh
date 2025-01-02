#!/usr/bin/env bash

# Script to print out the amount of memory used by the indexer. Prints out
# new value once a second.
PID=$(pgrep column)  # Change the name if the filename changes

while true; do
    ps_output=$(ps -o pid,rss -p $PID)
    if [ $? -ne 0 ]; then
        echo "Process no longer exists"
        break
    fi
    echo "$ps_output" | tail -n 1 | awk '{print "RSS:", $2/1024, "MB"}'
    sleep 1
done