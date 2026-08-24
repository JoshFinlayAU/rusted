RUSTED_API_TOKEN=testtoken123 /tmp/rtbin/rusted serve --addr 127.0.0.1:8099 --token testtoken123 &
sleep 2
echo "=== /api/transports ==="
curl -s http://127.0.0.1:8099/api/transports -H "Authorization: Bearer testtoken123"
echo ""
echo "=== /api/devices (list) ==="
curl -s http://127.0.0.1:8099/api/devices -H "Authorization: Bearer testtoken123"
echo ""
echo "=== /api/drivers ==="
curl -s http://127.0.0.1:8099/api/drivers -H "Authorization: Bearer testtoken123"
echo ""

Diagnostic curl commands:
# 1. Verify /api/transports works (should list ["ssh","ssh-exec","telnet"])
curl -s http://librenms.ssisnet.ssis/plugin/rusted/api/transports \
  -H "Cookie: $(grep Cookie <<< 'YOUR_BROWSER_COOKIES')"

# 2. Test the PUT endpoint directly (port as integer!)
curl -s -X PUT \
  http://librenms.ssisnet.ssis/plugin/rusted/api/devices/GA2960A35.mgt.ssis \
  -H "Content-Type: application/json" \
  -H "X-CSRF-TOKEN: YOUR_TOKEN" \
  -H "Cookie: YOUR_COOKIES" \
  -d '{"port":23}'

# 3. Same test but with string port (reproduces the bug):
curl -s -X PUT \
  http://librenms.ssisnet.ssis/plugin/rusted/api/devices/GA2960A35.mgt.ssis \
  -H "Content-Type: application/json" \
  -d '{"port":"23"}'
# → rusted returns 400 {"error":"invalid JSON"}

# 4. Verify the rusted API directly
curl -s http://127.0.0.1:8080/api/transports \
  -H "Authorization: Bearer YOUR_TOKEN"
curl -s http://127.0.0.1:8080/api/devices \
  -H "Authorization: Bearer YOUR_TOKEN"
# → should show last_backup + last_status fields
