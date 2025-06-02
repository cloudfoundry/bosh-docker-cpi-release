#!/usr/bin/env bash

# Common utilities for test scripts

# Print a clear section header with timestamp
header() {
    local title="$1"
    local width=80
    local timestamp=$(date '+%Y-%m-%d %H:%M:%S')
    
    echo
    echo
    printf '=%.0s' $(seq 1 $width)
    echo
    printf "| %-*s |\n" $((width - 4)) "$timestamp: $title"
    printf '=%.0s' $(seq 1 $width)
    echo
    echo
}

# Print a sub-header without timestamp
subheader() {
    local title="$1"
    local width=60
    
    echo
    printf -- '-%.0s' $(seq 1 $width)
    echo
    echo "$title"
    printf -- '-%.0s' $(seq 1 $width)
    echo
}

# Print success message
success() {
    local message="$1"
    local details="$2"
    echo -e "\033[32m✓ $message\033[0m"
    if [ -n "$details" ]; then
        echo "  $details"
    fi
}

# Print error message
error() {
    local message="$1"
    echo -e "\033[31m✗ $message\033[0m"
}

# Print warning message
warning() {
    local message="$1"
    local details="$2"
    echo -e "\033[33m⚠ $message\033[0m"
    if [ -n "$details" ]; then
        echo "  $details"
    fi
}

# Print info message
info() {
    local message="$1"
    echo -e "\033[34mℹ $message\033[0m"
}
