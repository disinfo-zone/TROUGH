# Rate Limiting Testing Guide

## Quick Testing

### 1. Test Rate Limiting on Login Endpoint
```bash
# This should work 10 times, then get rate limited
for i in {1..15}; do
  echo "Request $i:"
  curl -X POST http://localhost:8080/api/login \
    -H "Content-Type: application/json" \
    -d '{"email":"test@example.com","password":"wrong-password"}'
  echo ""
  sleep 0.1
done
```

### 2. Test Rate Limiting on Register Endpoint
```bash
# This should work 5 times, then get rate limited
for i in {1..8}; do
  echo "Request $i:"
  curl -X POST http://localhost:8080/api/register \
    -H "Content-Type: application/json" \
    -d '{"username":"testuser'$i'","email":"test'$i'@example.com","password":"Password123!"}'
  echo ""
  sleep 0.1
done
```

### 3. View Admin Stats (requires admin login)
```bash
# First login as admin
curl -X POST http://localhost:8080/api/login \
  -H "Content-Type: application/json" \
  -d '{"email":"admin@example.com","password":"your-admin-password"}'

# Extract token from response and use it
curl -X GET http://localhost:8080/api/admin/rate-limiter-stats \
  -H "Authorization: Bearer YOUR_ADMIN_TOKEN"
```

## Expected Behavior

### Successful Response (200 OK)
```json
{
  "total_entries": 5,
  "evicted_count": 0,
  "cleanup_count": 3,
  "denied_count": 12,
  "last_cleanup_time": "2025-09-07T15:53:26.123456789Z",
  "memory_usage_bytes": 440,
  "uptime": "30.456s"
}
```

### Rate Limited Response (429 Too Many Requests)
```json
{
  "error": "Too many requests"
}
```

## Configuration

The rate limiter uses these settings from `config.yaml`:

```yaml
rate_limiting:
  max_entries: 1000        # Maximum IP entries to store (~88KB max memory)
  cleanup_interval: 1m     # How often to clean up expired entries
  entry_ttl: 30m          # How long to keep IP entries before cleanup
  trusted_proxies: ["127.0.0.1", "::1"]  # Trusted proxy IPs
  enable_debug: false     # Enable debug logging
```

## Security Features Tested

1. **Memory Management**: Entries are automatically cleaned up
2. **LRU Eviction**: Oldest entries are removed when limit is reached
3. **IP Validation**: Proper handling of X-Forwarded-For headers
4. **Thread Safety**: No race conditions under high load
5. **Token Bucket**: Smooth rate limiting with refill mechanism