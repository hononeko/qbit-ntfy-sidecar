# Security Policy

## Supported Versions

Currently, only the latest release of `qbit-ntfy-sidecar` is supported with security updates. I encourage all users to stay up-to-date with the latest version.

| Version | Supported          |
| ------- | ------------------ |
| Latest  | :white_check_mark: |
| Older   | :x:                |

## ⚠️ Access Security Warning

**The sidecar defaults to denying all requests to `/track`.**
You **must** configure the `ALLOWED_SUBNETS` environment variable (e.g., `ALLOWED_SUBNETS=10.0.0.0/8,192.168.1.0/24`) to allow qBittorrent to trigger notifications.

The `/track` endpoint does not implement authentication. Exposure to untrusted networks without proper IP filtering may allow malicious actors to spam the endpoint, causing a Denial of Service (DoS) via resource exhaustion in your qBittorrent instance. Keep access strictly limited to the local container network and authorized subnets.

## Reporting a Vulnerability

I take the security of `qbit-ntfy-sidecar` seriously. If you discover a security vulnerability, please report it to me immediately.

**Please do not report security vulnerabilities through public GitHub issues.**

Instead, please report them via [GitHub Security Advisories](https://github.com/hononeko/qbit-ntfy-sidecar/security/advisories/new) or by reaching out to the repository owner directly.

Please include the following information in your report:

- A description of the vulnerability.
- Steps to reproduce the issue.
- Potential impact.
- Any suggested mitigations or fixes.

I will acknowledge receipt of your vulnerability report and strive to address it as quickly as possible.
