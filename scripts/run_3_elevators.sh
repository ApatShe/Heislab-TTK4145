#!/bin/bash
# run_3_elevators.sh
# Launches 3 simulators and 3 elevator processes, each in its own terminal.
# All share the same broadcast ports so peer discovery works across instances.
# Kill the entire session with: pkill -f SimElevatorServer; pkill -f heislab

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT="$SCRIPT_DIR/.."

NUM_FLOORS=$(grep 'num_floors'    "$ROOT/config.yml" | awk '{print $2}')
BASE_PORT=$(grep  'base_sim_port' "$ROOT/config.yml" | awk '{print $2}')
NUM_ELEVATORS=$(grep 'num_elevators' "$ROOT/config.yml" | awk '{print $2}')

cd "$ROOT"
go build -o heislab . 2>&1 || { echo "Build failed"; exit 1; }

# Pick terminal: override with ELEVATOR_TERM=xterm|kitty|gnome-terminal
if [ -n "${ELEVATOR_TERM:-}" ]; then
    TERM_BIN="$ELEVATOR_TERM"
elif command -v gnome-terminal &>/dev/null; then
    TERM_BIN="gnome-terminal"
elif command -v xterm &>/dev/null; then
    TERM_BIN="xterm"
elif command -v kitty &>/dev/null; then
    TERM_BIN="kitty"
else
    echo "No supported terminal emulator found. Set ELEVATOR_TERM=xterm|kitty|gnome-terminal"; exit 1
fi
echo "Using terminal: $TERM_BIN"

launch_term() {
    local title="$1"
    local cmd="$2"
    case "$TERM_BIN" in
        gnome-terminal) env -u LD_LIBRARY_PATH gnome-terminal --title="$title" -- bash -c "$cmd" ;;
        xterm)          xterm -T "$title" -e bash -c "$cmd" & ;;
        kitty)          kitty --title "$title" bash -c "$cmd" & ;;
    esac
}

for i in $(seq 0 $((NUM_ELEVATORS - 1))); do
    SIM_PORT=$((BASE_PORT + i))
    NODE_ID="elevator-$i"

    # Simulator — only start if not already bound to this port
    if ! pgrep -f "SimElevatorServer.*--port $SIM_PORT" > /dev/null 2>&1; then
        launch_term "Sim $NODE_ID" "$ROOT/ElevSimulator/SimElevatorServer --port $SIM_PORT --numfloors $NUM_FLOORS; exec bash"
        sleep 0.3
    else
        echo "Simulator already running on port $SIM_PORT — reusing it."
    fi

    # Elevator process — restart loop
    launch_term "Node $NODE_ID" "cd $ROOT; while true; do ./heislab --port $SIM_PORT --id $NODE_ID --local || true; echo 'Node $NODE_ID exited. Restarting in 1s...'; sleep 1; done; exec bash"
done
