#!/bin/bash

# Simple rate limiting test script
# Run this from the project directory

echo "=== Trough Rate Limiting Test ==="
echo ""

# Test 1: Register endpoint (should allow 5 requests, then block)
echo "1. Testing register endpoint (limit: 5 requests/minute)..."
for i in {1..8}; do
    response=$(curl -s -o /dev/null -w "%{http_code}" -X POST http://localhost:8080/api/register \
        -H "Content-Type: application/json" \
        -d "{\"username\":\"testuser$i\",\"email\":\"test$i@example.com\",\"password\":\"Password123!\"}")
    
    if [ $response -eq 429 ]; then
        echo "   Request $i: RATE LIMITED (429)"
    elif [ $response -eq 201 ]; then
        echo "   Request $i: SUCCESS (201)"
    else
        echo "   Request $i: OTHER ($response)"
    fi
    sleep 0.1
done

echo ""
echo "2. Testing login endpoint (limit: 10 requests/minute)..."
for i in {1..13}; do
    response=$(curl -s -o /dev/null -w "%{http_code}" -X POST http://localhost:8080/api/login \
        -H "Content-Type: application/json" \
        -d "{\"email\":\"test@example.com\",\"password\":\"wrong-password\"}")
    
    if [ $response -eq 429 ]; then
        echo "   Request $i: RATE LIMITED (429)"
    elif [ $response -eq 200 ]; then
        echo "   Request $i: SUCCESS (200)"
    else
        echo "   Request $i: OTHER ($response)"
    fi
    sleep 0.1
done

echo ""
echo "3. Testing forgot-password endpoint (limit: 3 requests/5 minutes)..."
for i in {1..5}; do
    response=$(curl -s -o /dev/null -w "%{http_code}" -X POST http://localhost:8080/api/forgot-password \
        -H "Content-Type: application/json" \
        -d "{\"email\":\"test@example.com\"}")
    
    if [ $response -eq 429 ]; then
        echo "   Request $i: RATE LIMITED (429)"
    elif [ $response -eq 200 ]; then
        echo "   Request $i: SUCCESS (200)"
    else
        echo "   Request $i: OTHER ($response)"
    fi
    sleep 0.1
done

echo ""
echo "=== Test Complete ==="
echo ""
echo "To view admin stats, log in as admin and visit:"
echo "  http://localhost:8080/api/admin/rate-limiter-stats"
echo ""
echo "Or use curl with your admin token:"
echo "  curl -H \"Authorization: Bearer YOUR_TOKEN\" http://localhost:8080/api/admin/rate-limiter-stats"