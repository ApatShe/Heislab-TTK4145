#!/bin/bash
# run_single.sh
# Starts one simulator (persistent) and one elevator process (restartable).
# Killing the elevator process (Ctrl-C or SIGKILL) does NOT kill the simulator.
# The elevator is automatically restarted after each exit.
set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT="$SCRIPT_DIR/.."

NUM_FLOORS=$(grep 'num_floors'    "$ROOT/config.yml" | awk '{print $2}')
BASE_PORT=$(grep  'base_sim_port' "$ROOT/config.yml" | awk '{print $2}')

SIM_PORT=$BASE_PORT
NODE_ID="${1:-elevator-0}"   # override with: ./run_single.sh elevator-1 15658

cd "$ROOT"
go build -o heislab . 2>&1 || { echo "Build failed"; exit 1; }

# Simulator in its own terminal — only started if not already running
if ! pgrep -f "SimElevatorServer.*--port $SIM_PORT" > /dev/null 2>&1; then
    gnome-terminal --title="Sim $NODE_ID" -- bash -c \
        "./ElevSimulator/SimElevatorServer --port $SIM_PORT --numfloors $NUM_FLOORS; exec bash"
    sleep 0.5
else
    echo "Simulator already running on port $SIM_PORT — reusing it."
fi

# Restart loop — keeps the elevator running across crashes/kills
echo "Starting elevator $NODE_ID on port $SIM_PORT (Ctrl-C to stop restart loop)"
while true; do
    ./heislab --port "$SIM_PORT" --id "$NODE_ID" --local || true
    echo "Elevator process exited. Rebuilding and restarting in 1 s..."
    sleep 1
    go build -o heislab . 2>&1 || echo "Build failed — retrying after next exit"
done
