#!/bin/bash

function status_line() {
    echo -e "\n### ${1} ###\n"
}

# Exit upon any error
set -e

status_line "Begin build..."

make all check lint format-check coverage docker-build-prod
