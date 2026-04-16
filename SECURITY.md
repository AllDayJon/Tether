# Security Policy

## Reporting a vulnerability

Please do not report security vulnerabilities through public GitHub issues.

Instead, use GitHub's private vulnerability reporting:
**[Report a vulnerability](https://github.com/AllDayJon/Tether/security/advisories/new)**

Include as much detail as you can: what the issue is, how to reproduce it, and what the potential impact is. You'll receive a response within 7 days.

## Scope

Tether is a local tool — it has no servers, no accounts, and no network traffic beyond the Claude Code CLI calling Anthropic's API. Security issues most likely to be relevant:

- Command injection or sandbox escapes in the command classifier (`internal/cmdguard/`)
- Unintended data exposure through the IPC socket or log files
- Shell integration scripts doing something unsafe at source time
