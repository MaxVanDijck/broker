---
title: Authentication
weight: 1
---

## Overview

Broker supports two authentication methods:

1. **OIDC** -- For human users. Integrates with any OpenID Connect provider (Okta, Google Workspace, Azure AD, Auth0).
2. **Shared token** -- For service accounts, CI/CD pipelines, and agents. A pre-shared secret via `BROKER_TOKEN`.

Both can be active simultaneously. OIDC is recommended for teams; the shared token is useful for automation.

## OIDC setup

### 1. Create an application in your identity provider

{{< tabs items="Okta,Google,Azure AD" >}}

{{< tab >}}
1. In the Okta Admin Console, go to **Applications > Create App Integration**
2. Select **OIDC - OpenID Connect**, then **Native Application** (public client)
3. Set the sign-in redirect URI to `http://localhost:0/callback` (the CLI uses a random port)
4. Under **Assignments**, assign the users or groups that should have access
5. Note the **Client ID** and your **Okta domain** (e.g. `https://dev-123456.okta.com`)
{{< /tab >}}

{{< tab >}}
1. In the Google Cloud Console, go to **APIs & Services > Credentials**
2. Click **Create Credentials > OAuth 2.0 Client ID**
3. Select **Desktop app** as the application type
4. Note the **Client ID**
5. The issuer URL is `https://accounts.google.com`
{{< /tab >}}

{{< tab >}}
1. In Azure Portal, go to **Azure Active Directory > App registrations > New registration**
2. Set redirect URI to `http://localhost` with type **Mobile and desktop applications**
3. Under **Authentication**, enable **Allow public client flows**
4. Note the **Application (client) ID** and **Directory (tenant) ID**
5. The issuer URL is `https://login.microsoftonline.com/{tenant-id}/v2.0`
{{< /tab >}}

{{< /tabs >}}

### 2. Configure the server

Add the OIDC configuration to `~/.broker/config.yaml`:

```yaml
auth:
  oidc:
    issuer: https://dev-123456.okta.com
    client_id: 0oa1234567890abcdef
```

Restart the server for changes to take effect.

### 3. Log in from the CLI

```bash
broker login --issuer https://dev-123456.okta.com --client-id 0oa1234567890abcdef
```

This opens a browser for authentication. After successful login, tokens are stored in `~/.broker/credentials.json` and automatically attached to all subsequent commands.

To set defaults so you don't need the flags every time:

```bash
export BROKER_OIDC_ISSUER=https://dev-123456.okta.com
export BROKER_OIDC_CLIENT_ID=0oa1234567890abcdef
broker login
```

### 4. Verify

```bash
broker status
```

If authentication is working, you'll see your clusters. If not, you'll get an "unauthorized" error.

### Log out

```bash
broker logout
```

## Shared token (BROKER_TOKEN)

For CI/CD pipelines, service accounts, and non-interactive environments:

```bash
export BROKER_TOKEN=my-secret-token
broker launch -c train task.yaml
```

Set the same token on the server:

```bash
BROKER_TOKEN=my-secret-token broker-server
```

The token is checked via HTTP Basic auth (`Authorization: Basic base64(broker:TOKEN)`).

## Dashboard authentication

When OIDC is configured, the dashboard login flow works through the server:

1. User visits the dashboard
2. Dashboard redirects to `GET /auth/login`
3. Server redirects to the OIDC provider
4. After authentication, the server returns tokens at `GET /auth/callback`
5. Dashboard stores the access token and includes it in API requests

## Authentication precedence

When a request arrives, the server checks authentication in this order:

1. `Authorization: Bearer <jwt>` -- validated against the OIDC provider's JWKS endpoint
2. `Authorization: Basic <base64>` -- validated against `BROKER_TOKEN`
3. If neither is configured, all requests are allowed (local development mode)

Unauthenticated endpoints (always accessible):
- `GET /healthz`
- `GET /readyz`
- `GET /agent/v1/binary`
- `GET /auth/login`
- `GET /auth/callback`
