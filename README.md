# LFX MCP Server

A [Model Context Protocol](https://modelcontextprotocol.io/) (MCP) server that connects AI assistants to the Linux Foundation's LFX platform.

## What You Can Do

- **Explore projects** — Search and retrieve details for any LFX project
- **Manage committees** — Search, create, update, and delete committees and their members across projects
- **Work with mailing lists** — Search mailing lists and their subscribers
- **Query members** — Search memberships by tier, status, organization, and more; get key contacts
- **Track meetings** — Find upcoming meetings, registrants, past participants, transcripts, and AI-generated summaries

## Connecting to the LFX MCP Server

The LFX MCP Server is available as a hosted, production service at:

```text
https://mcp.lfx.dev/mcp
```

This endpoint uses the [Streamable HTTP](https://modelcontextprotocol.io/specification/2025-03-26/basic/transports#streamable-http) transport with OAuth 2.0 authentication. You will be prompted to log in with your Linux Foundation account the first time you connect.

> **Note:** Running the LFX MCP Server locally (e.g. in stdio mode) is not a supported end-user configuration. The full tool set requires OAuth authentication flows that are only available through the hosted service.

**Linux Foundation SSO does not support Dynamic Client Registration (DCR) or Client ID Metadata Documents (CIMD) at this time.** Please file an issue to request additional client support.

### OpenCode

Add the following to your `~/.config/opencode/opencode.json`:

```json
{
  "$schema": "https://opencode.ai/config.json",
  "mcp": {
    "lfx-mcp-server": {
      "type": "remote",
      "url": "https://mcp.lfx.dev/mcp",
      "enabled": true,
      "oauth": {
        "clientId": "LnBd9qGpwjXNs26aZxeXSkTCs0ac4zgM"
      }
    }
  }
}
```

See the [OpenCode MCP documentation](https://opencode.ai/docs/mcp-servers) for more details.

### Claude

Add the LFX MCP Server in **Settings → Integrations → Add Integration**:

- **URL:** `https://mcp.lfx.dev/mcp`
- **Client ID:** `Ef9tuU5wcJJIXmNGvZyGUkZFfD8CZWar`

### Cursor

Add the following to your `~/.cursor/mcp.json`:

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

### Additional clients (via mcp-remote)

If your MCP client does not support OAuth 2.0 with Streamable HTTP, you can use [mcp-remote](https://github.com/geelen/mcp-remote) as a local proxy. It handles the OAuth flow in your browser and serves a `stdio` transport to your MCP client.

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
        "{\"client_id\":\"tjrXD5ZJORf6rpngMSRqqPmf3W1bnHEV\"}"
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
- **Authentication → OAuth 2.0 Flow → Client ID:** `4ibLLbnz9kwMEcE3RUCUH51F0RS3Hx3O`

Before hitting **Connect**, follow the **Open Auth Settings** button, then select **Quick OAuth Flow**.

## Available Tools

### Projects

| Tool              | Description                                                   |
|-------------------|---------------------------------------------------------------|
| `search_projects` | Search for LFX projects by name with typeahead and pagination |
| `get_project`     | Get a project's base info and settings by UID                 |

### Committees

| Tool                        | Description                                                                               |
|-----------------------------|-------------------------------------------------------------------------------------------|
| `search_committees`         | Search for committees by name; optionally filter by project                               |
| `get_committee`             | Get a committee's base info and settings by UID                                           |
| `create_committee`          | Create a new committee under a project                                                    |
| `update_committee`          | Update a committee's base information                                                     |
| `update_committee_settings` | Update a committee's settings (visibility, email requirements, meeting attendee defaults) |
| `delete_committee`          | Delete a committee by UID                                                                 |
| `search_committee_members`  | Search committee members; filter by committee, project, or name                           |
| `get_committee_member`      | Get a specific committee member by committee and member UID                               |
| `create_committee_member`   | Add a new member to a committee                                                           |
| `update_committee_member`   | Update an existing committee member's information                                         |
| `delete_committee_member`   | Remove a member from a committee                                                          |

### Mailing Lists

| Tool                          | Description                                                    |
|-------------------------------|----------------------------------------------------------------|
| `search_mailing_lists`        | Search for mailing lists by name; optionally filter by project |
| `get_mailing_list`            | Get a mailing list's base info and settings by UID             |
| `get_mailing_list_service`    | Get a mailing list service's base info and settings by UID     |
| `search_mailing_list_members` | Search mailing list members; filter by list, project, or name  |
| `get_mailing_list_member`     | Get a specific mailing list member by list and member UID      |

### Members

| Tool                          | Description                                                                     |
|-------------------------------|---------------------------------------------------------------------------------|
| `search_members`              | Search and filter members (memberships) by status, tier, organization, and more |
| `get_member_membership`       | Get a single member's membership details by member and membership ID            |
| `get_membership_key_contacts` | Get key contacts (primary contacts, board members) for a membership             |

### Meetings

| Tool                         | Description                                                       |
|------------------------------|-------------------------------------------------------------------|
| `search_meetings`            | Search for meetings; filter by project, committee, date range     |
| `get_meeting`                | Get a meeting by UID                                              |
| `search_meeting_registrants` | Search meeting registrants; filter by meeting, committee, project |
| `get_meeting_registrant`     | Get a meeting registrant by UID                                   |

### Past Meeting Data

| Tool                               | Description                                                             |
|------------------------------------|-------------------------------------------------------------------------|
| `search_past_meeting_participants` | Search past meeting participants; filter by meeting, committee, project |
| `get_past_meeting_participant`     | Get a past meeting participant by UID                                   |
| `search_past_meeting_transcripts`  | Search past meeting transcripts; filter by meeting, committee, project  |
| `get_past_meeting_transcript`      | Get a past meeting transcript by UID                                    |
| `search_past_meeting_summaries`    | Search past meeting summaries; filter by meeting, committee, project    |
| `get_past_meeting_summary`         | Get a past meeting summary by UID                                       |

### Utility

| Tool          | Description                                         |
|---------------|-----------------------------------------------------|
| `hello_world` | Simple greeting tool for testing MCP connectivity   |
| `user_info`   | Get the authenticated user's OpenID Connect profile |

## License

Copyright The Linux Foundation and each contributor to LFX.

This project's source code is licensed under the MIT License. A copy of the
license is available in LICENSE.

This project's documentation is licensed under the Creative Commons Attribution
4.0 International License \(CC-BY-4.0\). A copy of the license is available in
LICENSE-docs.
