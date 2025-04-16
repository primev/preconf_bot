#!/bin/bash

PRECONF_BOT_DIR=/home/user/preconf_bot

CONTAINER=$(docker ps --filter "name=preconf_bot-mev-commit-bidder" --format "{{.Names}}")
if [ -z "$CONTAINER" ]; then
    echo "Container not found"
    exit 1
fi

HEALTH=$(docker inspect --format='{{.State.Health.Status}}' "$CONTAINER")
if [ "$HEALTH" = "unhealthy" ]; then
    echo "Container $CONTAINER is unhealthy. Restarting..."
    # TODO: slack alert
    echo "Restarting docker compose app..."
    cd $PRECONF_BOT_DIR || { echo "Failed to change directory to $PRECONF_BOT_DIR"; exit 1; }
    docker compose down
    docker compose up --build -d
    echo "Docker compose app restarted."

elif [ "$HEALTH" = "healthy" ]; then
    echo "Docker compose app is healthy."
elif [ "$HEALTH" = "starting" ]; then
    echo "Docker compose app is starting..."
else
    echo "Docker compose app is in an unknown state: $HEALTH"
fi
