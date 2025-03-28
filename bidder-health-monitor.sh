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

    echo "Cancelling auto deposit..."
    curl -X POST http://localhost:13523/v1/bidder/cancel_auto_deposit?withdraw=true
    
    echo "Polling for auto deposit status..."
    MAX_ATTEMPTS=42  # 7 minute timeout w/ 10 second intervals
    ATTEMPT=0
    while [ $ATTEMPT -lt $MAX_ATTEMPTS ]; do
        STATUS=$(curl -s http://localhost:13523/v1/bidder/auto_deposit_status)
        
        EMPTY_WINDOW_BALANCES=$(echo "$STATUS" | grep -o '"windowBalances":\[\]' || echo "")
        AUTO_DEPOSIT_DISABLED=$(echo "$STATUS" | grep -o '"isAutodepositEnabled":false' || echo "")
        
        if [ -n "$EMPTY_WINDOW_BALANCES" ] && [ -n "$AUTO_DEPOSIT_DISABLED" ]; then
            echo "Auto deposit successfully cancelled and withdrawn."
            break
        fi
        echo "Auto deposit still active, waiting 10 seconds before checking again..."
        sleep 10
        ATTEMPT=$((ATTEMPT + 1))
    done
    if [ $ATTEMPT -eq $MAX_ATTEMPTS ]; then
        echo "Warning: Timed out waiting for auto deposit to cancel. Proceeding with restart anyway."
    fi
    echo "Restarting docker compose app..."
    cd $PRECONF_BOT_DIR || { echo "Failed to change directory to $PRECONF_BOT_DIR"; exit 1; }
    docker compose down
    docker compose up --build -d
    echo "Docker compose app restarted."
fi
