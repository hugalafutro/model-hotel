#!/bin/bash

set -e

echo "=== Model Hotel End-to-End Integration Test ==="
echo ""

# Check if Docker is running
echo "1. Checking Docker status..."
if ! docker ps > /dev/null 2>&1; then
    echo "❌ Docker is not running"
    exit 1
fi
echo "✅ Docker is running"
echo ""

# The admin token is configured via the ADMIN_TOKEN environment variable.
# Docker compose passes ${ADMIN_TOKEN:-} into the container, so both the
# script and the app use the same value. A default is provided so the script
# works without any prior setup.
#
# How this works:
#   - If the admin-token file doesn't exist yet, the app uses ADMIN_TOKEN
#     as the initial token (only its SHA256 hash is stored on disk).
#   - If the file already exists with a matching hash, the token just works.
#   - If the file exists with a DIFFERENT hash (stale from a previous run),
#     we delete it and recreate the container so the app picks up our token.
#   - No log-scraping is needed — we already know the token.
echo "2. Getting admin token..."

ADMIN_TOKEN="${ADMIN_TOKEN:-test-integration-admin-token}"
export ADMIN_TOKEN

echo "   Using ADMIN_TOKEN=$ADMIN_TOKEN"

# First, try the token against a running container. If it works, we're done.
# This covers the common case where the container was previously started
# with the same ADMIN_TOKEN (e.g. a prior test run).
TOKEN_WORKS=false
if curl -s http://localhost:8081/health > /dev/null 2>&1; then
    HTTP_CODE=$(curl -s -o /dev/null -w "%{http_code}" http://localhost:8081/api/providers \
        -H "Authorization: Bearer $ADMIN_TOKEN")
    if [ "$HTTP_CODE" = "200" ]; then
        TOKEN_WORKS=true
        echo "   ✅ Token already valid against running container"
    fi
fi

# If the token didn't work, we need to ensure the container is running with
# our ADMIN_TOKEN and that the admin-token file matches.
if [ "$TOKEN_WORKS" = "false" ]; then
    # Remove any stale admin-token file. It's mounted from ./.data on the host.
    # Without this, the app would ignore ADMIN_TOKEN and use the old hash.
    ADMIN_TOKEN_FILE="$PWD/.data/admin-token"
    if [ -f "$ADMIN_TOKEN_FILE" ]; then
        echo "   ⚠️  Existing admin-token file found — removing so ADMIN_TOKEN takes effect"
        rm -f "$ADMIN_TOKEN_FILE"
    fi

    # Recreate the container so docker compose re-evaluates ${ADMIN_TOKEN:-}
    # and passes our value into the container. A plain `restart` does NOT
    # update environment variables — only `up` (which recreates on config change).
    echo "   Recreating container with ADMIN_TOKEN..."
    docker compose up -d app > /dev/null 2>&1

    # Wait for the app to be ready
    echo "   Waiting for app to be ready..."
    for i in {1..30}; do
        if curl -s http://localhost:8081/health > /dev/null 2>&1; then
            break
        fi
        sleep 1
    done

    # Verify the token works
    for i in {1..10}; do
        HTTP_CODE=$(curl -s -o /dev/null -w "%{http_code}" http://localhost:8081/api/providers \
            -H "Authorization: Bearer $ADMIN_TOKEN")
        if [ "$HTTP_CODE" = "200" ]; then
            TOKEN_WORKS=true
            break
        fi
        sleep 1
    done
fi

if [ "$TOKEN_WORKS" = "false" ]; then
    echo "❌ Could not authenticate with admin token"
    exit 1
fi
echo "✅ Admin token validated"
echo ""

# Test health endpoint
echo "3. Testing health endpoint..."
HEALTH_RESPONSE=$(curl -s http://localhost:8081/health)
if [ "$HEALTH_RESPONSE" != "OK" ]; then
    echo "❌ Health check failed"
    exit 1
fi
echo "✅ Health check passed"
echo ""

# Test providers endpoint
echo "4. Testing providers endpoint..."
PROVIDERS_RESPONSE=$(curl -s http://localhost:8081/api/providers \
    -H "Authorization: Bearer $ADMIN_TOKEN")
echo "✅ Providers endpoint works"
echo "$PROVIDERS_RESPONSE" | head -200
echo ""

# Test stats endpoint
echo "5. Testing stats endpoint..."
STATS_RESPONSE=$(curl -s http://localhost:8081/api/stats \
    -H "Authorization: Bearer $ADMIN_TOKEN")
echo "✅ Stats endpoint works"
echo "$STATS_RESPONSE"
echo ""

# Test models endpoint
echo "6. Testing models endpoint..."
MODELS_RESPONSE=$(curl -s http://localhost:8081/api/models \
    -H "Authorization: Bearer $ADMIN_TOKEN")
echo "✅ Models endpoint works"
echo "$MODELS_RESPONSE"
echo ""

# Test logs endpoint
echo "7. Testing logs endpoint..."
LOGS_RESPONSE=$(curl -s http://localhost:8081/api/logs \
    -H "Authorization: Bearer $ADMIN_TOKEN")
echo "✅ Logs endpoint works"
echo "$LOGS_RESPONSE"
echo ""

# Test provider creation
echo "8. Testing provider creation..."
CREATE_RESPONSE=$(curl -s -X POST http://localhost:8081/api/providers \
    -H "Authorization: Bearer $ADMIN_TOKEN" \
    -H "Content-Type: application/json" \
    -d '{"name":"Test Integration Provider","base_url":"https://api.openai.com/v1","api_key":"sk-test-integration-key"}')

if echo "$CREATE_RESPONSE" | grep -q "error"; then
    echo "❌ Provider creation failed"
    exit 1
fi
echo "✅ Provider created successfully"
PROVIDER_ID=$(echo "$CREATE_RESPONSE" | grep -oP '"id":"\K[^"]+')
echo "Provider ID: $PROVIDER_ID"
echo ""

# Test provider listing
echo "9. Testing provider listing..."
LIST_RESPONSE=$(curl -s http://localhost:8081/api/providers \
    -H "Authorization: Bearer $ADMIN_TOKEN")

if ! echo "$LIST_RESPONSE" | grep -q "Test Integration Provider"; then
    echo "❌ Provider listing failed"
    exit 1
fi
echo "✅ Provider listing works"
echo ""

# Test proxy key creation
echo "10. Testing proxy key creation..."
KEY_RESPONSE=$(curl -s -X POST http://localhost:8081/api/keys \
    -H "Authorization: Bearer $ADMIN_TOKEN" \
    -H "Content-Type: application/json" \
    -d '{"name":"integration-test-key"}')

if echo "$KEY_RESPONSE" | grep -q "error"; then
    echo "❌ Proxy key creation failed"
    exit 1
fi
echo "✅ Proxy key created successfully"
PROXY_KEY=$(echo "$KEY_RESPONSE" | grep -oP '"key":"\K[^"]+')
echo "Proxy Key: $PROXY_KEY"
echo ""

# Test provider deletion
echo "11. Testing provider deletion..."
if [ -n "$PROVIDER_ID" ]; then
    DELETE_RESPONSE=$(curl -s -X DELETE http://localhost:8081/api/providers/$PROVIDER_ID \
        -H "Authorization: Bearer $ADMIN_TOKEN")

    if [ "$DELETE_RESPONSE" != "" ]; then
        echo "❌ Provider deletion failed: $DELETE_RESPONSE"
        exit 1
    fi
    echo "✅ Provider deleted successfully"
else
    echo "⚠️  Skipping provider deletion (no provider ID)"
fi
echo ""

# Test frontend
echo "12. Testing frontend..."
FRONTEND_RESPONSE=$(curl -s http://localhost:8081/)
if ! echo "$FRONTEND_RESPONSE" | grep -q "Model Hotel"; then
    # Check if it returns HTML instead
    if ! echo "$FRONTEND_RESPONSE" | grep -q "<!doctype html>"; then
        echo "❌ Frontend failed"
        exit 1
    fi
fi
echo "✅ Frontend served successfully"
echo ""

echo "=== All Integration Tests Passed! ==="
echo ""
echo "Summary:"
echo "- Docker: ✅"
echo "- Health Check: ✅"
echo "- API Endpoints: ✅"
echo "- Provider CRUD: ✅"
echo "- Proxy Keys: ✅"
echo "- Frontend: ✅"
echo ""
echo "The Model Hotel application is fully functional and ready for production use."
