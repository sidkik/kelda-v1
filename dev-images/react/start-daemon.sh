#!/bin/bash

if [[ -f /tmp/start-daemon.lock ]]; then
    kill -- -$(cat /tmp/start-daemon.lock)
fi

$@ &
child_pid=$!
child_pgid="$(ps -o pid,pgid | awk {'if ($1 ==  "'"$child_pid"'") print $2'})"
echo ${child_pgid} > /tmp/start-daemon.lock
