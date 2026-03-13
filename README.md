# LFX MCP Server

A [Model Context Protocol](https://modelcontextprotocol.io/) (MCP) server that connects AI assistants to the Linux Foundation's LFX platform.

## What You Can Do

- **Explore projects** — Search and retrieve details for any LFX project
- **Manage committees** — Look up committees, their members, and roles across projects
- **Work with mailing lists** — Search mailing lists and their subscribers
- **Query members** — Search memberships by tier, status, organization, and more; get key contacts
- **Track meetings** — Find upcoming meetings, registrants, past participants, transcripts, and AI-generated summaries

## Connecting to the LFX MCP Server

The LFX MCP Server is available as a hosted, production service at:

```
https://mcp.lfx.dev/mcp
```

This endpoint uses the [Streamable HTTP](https://modelcontextprotocol.io/specification/2025-03-26/basic/transports#streamable-http) transport with OAuth 2.0 authentication. You will be prompted to log in with your Linux Foundation account the first time you connect.

> **Note:** Running the LFX MCP Server locally (e.g. in stdio mode) is not a supported end-user configuration. The full tool set requires OAuth authentication flows that are only available through the hosted service.

### Claude

Claude supports remote MCP servers directly. Add the LFX MCP Server in **Settings → Integrations → Add Integration**:

- **URL:** `https://mcp.lfx.dev/mcp`
- **Client ID:** `Ef9tuU5wcJJIXmNGvZyGUkZFfD8CZWar`

### Cursor

Cursor supports remote MCP servers with static OAuth. Add the following to your `~/.cursor/mcp.json`:

```json
{
  "mcpServers": {
    "lfx": {
      "url": "https://mcp.lfx.dev/mcp",
      "auth": {
        "CLIENT_ID": "HwGWwUy4uvqVQYDsuXb9IVamzcUhdZj5"
      }
    }
  }
}
```

### stdio-only clients (via mcp-remote)

If your MCP client only supports stdio-based servers, you can use [mcp-remote](https://github.com/geelen/mcp-remote) as a local proxy. It handles the OAuth flow in your browser and bridges stdio to the remote HTTP server.

The port `3334` is required so that the OAuth callback URL matches the registered client. Please refer to your client's documentation for the exact configuration syntax. For example, in **Zed** (`~/.config/zed/settings.json`):

```json
{
  "context_servers": {
    "lfx": {
      "command": "npx",
      "args": [
        "mcp-remote",
        "https://mcp.lfx.dev/mcp",
        "3334",
        "--static-oauth-client-info",
        "{\"client_id\":\"TODO: fill in after auth0-terraform deployment\"}"
      ]
    }
  }
}
```

A browser window will open for authentication on first use. To clear cached credentials (e.g. to re-authenticate):

```bash
rm -rf ~/.mcp-auth
```

### MCP Inspector (developer testing)

[MCP Inspector](https://github.com/modelcontextprotocol/inspector) is a browser-based tool for exploring and testing MCP servers. To connect it to the LFX MCP Server:

```bash
npx @modelcontextprotocol/inspector
```

Then in the Inspector UI:

- **Transport:** Streamable HTTP
- **URL:** `https://mcp.lfx.dev/mcp`
- **Client ID:** `TODO: fill in after auth0-terraform deployment`

## Available Tools

### Projects

| Tool | Description |
|------|-------------|
| `search_projects` | Search for LFX projects by name with typeahead and pagination |
| `get_project` | Get a project's base info and settings by UID |

### Committees

| Tool | Description |
|------|-------------|
| `search_committees` | Search for committees by name; optionally filter by project |
| `get_committee` | Get a committee's base info and settings by UID |
| `search_committee_members` | Search committee members; filter by committee, project, or name |
| `get_committee_member` | Get a specific committee member by committee and member UID |

### Mailing Lists

| Tool | Description |
|------|-------------|
| `search_mailing_lists` | Search for mailing lists by name; optionally filter by project |
| `get_mailing_list` | Get a mailing list's base info and settings by UID |
| `get_mailing_list_service` | Get a mailing list service's base info and settings by UID |
| `search_mailing_list_members` | Search mailing list members; filter by list, project, or name |
| `get_mailing_list_member` | Get a specific mailing list member by list and member UID |

### Members

| Tool | Description |
|------|-------------|
| `search_members` | Search and filter members (memberships) by status, tier, organization, and more |
| `get_member_membership` | Get a single member's membership details by member and membership ID |
| `get_membership_key_contacts` | Get key contacts (primary contacts, board members) for a membership |

### Meetings

| Tool | Description |
|------|-------------|
| `search_meetings` | Search for meetings; filter by project, committee, date range |
| `get_meeting` | Get a meeting by UID |
| `search_meeting_registrants` | Search meeting registrants; filter by meeting, committee, project |
| `get_meeting_registrant` | Get a meeting registrant by UID |

### Past Meeting Data

| Tool | Description |
|------|-------------|
| `search_past_meeting_participants` | Search past meeting participants; filter by meeting, committee, project |
| `get_past_meeting_participant` | Get a past meeting participant by UID |
| `search_past_meeting_transcripts` | Search past meeting transcripts; filter by meeting, committee, project |
| `get_past_meeting_transcript` | Get a past meeting transcript by UID |
| `search_past_meeting_summaries` | Search past meeting summaries; filter by meeting, committee, project |
| `get_past_meeting_summary` | Get a past meeting summary by UID |

### Utility

| Tool | Description |
|------|-------------|
| `hello_world` | Simple greeting tool for testing MCP connectivity |
| `user_info` | Get the authenticated user's OpenID Connect profile |

## License

Copyright The Linux Foundation and each contributor to LFX.

This project's source code is licensed under the MIT License. A copy of the
license is available in LICENSE.

This project's documentation is licensed under the Creative Commons Attribution
4.0 International License \(CC-BY-4.0\). A copy of the license is available in
LICENSE-docs.