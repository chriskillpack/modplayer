#!/usr/bin/env bash

# Check if an executable is provided as argument
if [ $# -lt 1 ]; then
    echo "Usage: $0 <executable> [arguments...]"
    exit 1
fi

# Capture the full command with arguments
cmd_with_args=("$@")

count=0
max_runs=25

while [ $count -lt $max_runs ]; do
    # Run the command with all its arguments
    "${cmd_with_args[@]}"
    
    # Increment counter
    ((count++))
    
    # Show remaining runs
    remaining=$((max_runs - count))
    echo "Completed run $count of $max_runs ($remaining remaining)"
    
    # Prompt for continuation if not at max runs
    if [ $count -lt $max_runs ]; then
        read -p "Run again? (y/Y to continue): " response
        if [[ ! $response =~ ^[yY]$ ]]; then
            echo "Exiting after $count runs"
            exit 0
        fi
    fi
done

echo "Completed all $max_runs runs"