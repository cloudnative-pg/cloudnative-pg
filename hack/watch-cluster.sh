#!/bin/bash
#
sn=CNPG

tmux kill-session -s "$sn"
tmux new-session -s "$sn" -n general -d

tmux split-window -v
tmux split-window -v
tmux select-pane -t "$sn:0.1"
tmux split-window -h

sleep 2s
tmux send-keys -t "$sn:0.0" C-z 'while true; do reset; kubectl -n cnpg-system logs -l app.kubernetes.io/name=cloudnative-pg -f; sleep 5; done' Enter
tmux send-keys -t "$sn:0.1" C-z 'watch -n 1 kubectl -n cnpg-system get cluster -A' Enter
tmux send-keys -t "$sn:0.2" C-z 'watch kubectl get podmonitor -A -o yaml' Enter

tmux -2 attach-session -d
