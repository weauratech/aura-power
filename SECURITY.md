# Security Policy

## Reporting a Vulnerability

If you discover a security vulnerability in Aura Power, please report it responsibly.

**Do NOT open a public GitHub issue for security vulnerabilities.**

Instead, please email: **security@weaura.tech**

Include:
- Description of the vulnerability
- Steps to reproduce
- Potential impact
- Suggested fix (if any)

We will acknowledge receipt within 48 hours and provide a timeline for resolution.

## Supported Versions

| Version | Supported |
|---------|-----------|
| 2.x     | Yes       |
| 1.x     | No        |

## Security Best Practices

When deploying Aura Power:

- Always set `JWT_SECRET` to a strong random value (32+ characters)
- Use HTTPS/TLS for the server (via Ingress or Gateway API)
- Enable NetworkPolicies (`networkPolicy.enabled: true`)
- Use the split architecture (server + controller with separate RBAC)
- Regularly rotate the admin password
- Keep the Helm chart and images updated
