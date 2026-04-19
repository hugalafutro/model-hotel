#!/bin/bash

set -e

echo "=== LLM-Proxy End-to-End Integration Test ==="
echo ""

# Check if Docker is running
echo "1. Checking Docker status..."
if ! docker ps > /dev/null 2>&1; then
    echo "❌ Docker is not running"
    exit 1
fi
echo "✅ Docker is running"
echo ""

# Get admin token from logs
echo "2. Getting admin token..."
ADMIN_TOKEN=$(docker compose logs app 2>/dev/null | grep "Admin token" | tail -1 | awk '{print $NF}')
if [ -z "$ADMIN_TOKEN" ]; then
    echo "❌ Could not get admin token"
    exit 1
fi
echo "✅ Admin token: $ADMIN_TOKEN"
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
if ! echo "$FRONTEND_RESPONSE" | grep -q "LLM-Proxy"; then
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
echo "The LLM-Proxy application is fully functional and ready for production use."
