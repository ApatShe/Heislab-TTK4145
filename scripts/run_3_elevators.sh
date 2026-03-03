#!/bin/bash
# run_3_elevators.sh
# Launches 3 simulators and 3 elevator processes, each in its own terminal.
# All share the same broadcast ports so peer discovery works across instances.
# Kill the entire session with: pkill -f SimElevatorServer; pkill -f "go run"
set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT="$SCRIPT_DIR/.."

NUM_FLOORS=$(grep 'num_floors'   "$ROOT/config.yml" | awk '{print $2}')
BASE_PORT=$(grep  'base_sim_port' "$ROOT/config.yml" | awk '{print $2}')
NUM_ELEVATORS=$(grep 'num_elevators' "$ROOT/config.yml" | awk '{print $2}')

cd "$ROOT"
go build -o heislab . 2>&1 || { echo "Build failed"; exit 1; }

for i in $(seq 0 $((NUM_ELEVATORS - 1))); do
    SIM_PORT=$((BASE_PORT + i))
    NODE_ID="elevator-$i"

    # Simulator — survives if the Go process dies
    gnome-terminal --title="Sim $NODE_ID" -- bash -c \
        "./ElevSimulator/SimElevatorServer --port $SIM_PORT --numfloors $NUM_FLOORS; exec bash"

    sleep 0.3   # give simulator time to bind the port

    # Elevator process
    gnome-terminal --title="Node $NODE_ID" -- bash -c \
        "./heislab --port $SIM_PORT --id $NODE_ID; exec bash"
done
