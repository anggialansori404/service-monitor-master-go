#!/bin/bash

# Configuration
BINARY_NAME="monitoring-server-linux-amd64.bin"
LOG_DIR="logs"
PID_FILE="service.pid"

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# Create logs directory if it doesn't exist
mkdir -p "$LOG_DIR"

get_pid() {
    if [ -f "$PID_FILE" ]; then
        cat "$PID_FILE"
    fi
}

is_running() {
    local pid=$(get_pid)
    if [ -n "$pid" ] && ps -p "$pid" > /dev/null 2>&1; then
        return 0
    else
        return 1
    fi
}

start() {
    if is_running; then
        echo -e "${YELLOW}Service is already running (PID: $(get_pid))${NC}"
        return
    fi

    # Daily log file name
    LOG_FILE="$LOG_DIR/$(date +%Y-%m-%d).log"
    
    echo -e "${GREEN}Starting $BINARY_NAME...${NC}"
    echo "Logs will be saved to: $LOG_FILE"
    
    # Run in background with nohup and redirect output to daily log
    nohup ./"$BINARY_NAME" >> "$LOG_FILE" 2>&1 &
    
    # Save PID
    echo $! > "$PID_FILE"
    
    if is_running; then
        echo -e "${GREEN}Service started successfully (PID: $(cat "$PID_FILE"))${NC}"
    else
        echo -e "${RED}Failed to start service${NC}"
    fi
}

stop() {
    if ! is_running; then
        echo -e "${YELLOW}Service is not running${NC}"
        # Remove stale PID file if exists
        [ -f "$PID_FILE" ] && rm "$PID_FILE"
        return
    fi

    local pid=$(get_pid)
    echo -e "${YELLOW}Stopping service (PID: $pid)...${NC}"
    kill "$pid"
    
    # Wait for process to stop
    for i in {1..5}; do
        if ! is_running; then
            break
        fi
        sleep 1
    done

    if is_running; then
        echo -e "${RED}Service failed to stop gracefully, forcing...${NC}"
        kill -9 "$pid"
        sleep 1
    fi

    rm "$PID_FILE"
    echo -e "${GREEN}Service stopped${NC}"
}

status() {
    if is_running; then
        echo -e "${GREEN}Service is running (PID: $(get_pid))${NC}"
        echo "Current log file: $LOG_DIR/$(date +%Y-%m-%d).log"
    else
        echo -e "${RED}Service is not running${NC}"
    fi
}

restart() {
    stop
    sleep 2
    start
}

case "$1" in
    start)
        start
        ;;
    stop)
        stop
        ;;
    status)
        status
        ;;
    restart)
        restart
        ;;
    *)
        echo "Usage: $0 {start|stop|status|restart}"
        exit 1
        ;;
esac
