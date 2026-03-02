## IP Blacklisting Implementation
- Integrated IP Blacklisting to mitigate scanning bots.
- Used MongoDB collection 'blacklisted_ips'.
- Suspicious patterns detected: index.php, .php, .cgi, .env, wp-admin, .git, and root .json files.
- IPBlacklist middleware added as the very first middleware to block blacklisted IPs immediately.
- SuspiciousPathLogger middleware now automatically blacklists IPs on detection.
- Localhost (127.0.0.1, ::1) is exempted from blacklisting.
## Middleware Cleanup
- Removed duplicate middleware registrations in cmd/server/main.go.
- Verified the correct global middleware order:
  1. IPBlacklist
  2. SecurityHeaders
  3. GlobalRateLimiter
  4. SuspiciousPathLogger
