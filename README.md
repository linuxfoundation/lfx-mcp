# LFX MCP Server

A [Model Context Protocol](https://modelcontextprotocol.io/) (MCP) server that connects AI assistants to the Linux Foundation's LFX platform.

## Features

- **Explore projects** — Search and retrieve details for Linux Foundation projects
- **Manage committees** — Search, create, update, and delete project committees and their members
- **Work with mailing lists** — Search project mailing lists and their subscribers
- **Track project meetings** — Find upcoming meetings, registrants, past participants, and AI-generated summaries
- **Query membership** — Search project memberships by tier, status, organization, and more; get and manage key contacts
- **Analyze data with LFX Lens** — Compare and report on project activities and contributions over time
- ... and more!

## Connecting to the LFX MCP Server

The LFX MCP Server is available as a hosted, production service at:

```text
https://mcp.lfx.dev/mcp
```

You will be prompted to log in with your Linux Foundation account (LFID) the first time you connect. *All MCP permissions correspond to LFX platform permissions granted to your LFID.*

**The following clients are set up to work with the LFX MCP Server.** Please file an issue to request additional client support. Running the LFX MCP Server as a local (stdio) MCP server is not supported at this time.

### Goose

#### GUI (one-click install)

Copy-paste into your browser location bar:

```text
goose://extension?url=https%3A%2F%2Fmcp.lfx.dev%2Fmcp&type=streamable_http&id=lfx&name=LFX&description=LFX%20MCP%20Server
```

Start a new chat and Goose will open a browser window for LFID login.

#### CLI

1. Run `goose configure`.
2. Select **Add Extension** → **Remote Extension (Streamable HTTP)**.
3. Enter `LFX` as the name.
4. Enter `https://mcp.lfx.dev/mcp` as the URI.
5. Enter `300` for the timeout.
6. Enter `LFX MCP Server` as the description.
7. Select **No** for adding custom headers.

After it acknowledges that your configuration was saved, running `goose` will open a browser window for LFID login.

### OpenCode

*OpenCode requires a client ID. The following client ID only works with OpenCode.*

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

### Zed

In Zed, open the Agent panel settings (ellipsis button) → **Add Custom Server** → **Remote**. Enter the following:

```json
{
  "lfx": {
    "url": "https://mcp.lfx.dev/mcp"
  }
}
```

Click **Add Server** → **Authenticate** to open a browser window for LFID login.

### Claude (LF enterprise)

If you use the Linux Foundation's enterprise Claude.ai organization, the LFX MCP Server is already provisioned as a connector in both Claude Desktop and Claude Code.

#### Claude Desktop

1. From the sidebar, select **Customize** → **Connectors**.
2. Find **LFX** in the list.
3. Click **Connect** and sign in with your LFID.

#### Claude Code

Run `/mcp` inside Claude Code and select **claude.ai LFX** → **Authenticate**. Claude Code will open a browser window for LFID login.

### Claude Desktop (personal account)

1. From the sidebar, select **Customize** → **Connectors**.
2. Use the **+** button and select **Add Custom Connector**.
3. Enter **LFX** and the URL `https://mcp.lfx.dev/mcp`.
4. Press **Add**, then **Connect** and sign in with your LFID.

### Claude Code (personal account)

```bash
claude mcp add --transport http lfx https://mcp.lfx.dev/mcp
```

Run `/mcp` inside Claude Code and select **lfx** → **Authenticate**. Claude Code will open a browser for LFID login.

### Visual Studio Code

Run **MCP: Open User Configuration** to open the `mcp.json` file to add the LFX MCP server in your user-level config:

```json
{
  "servers": {
    "lfx": {
      "type": "http",
      "url": "https://mcp.lfx.dev/mcp"
    }
  }
}
```

VS Code will prompt you to trust the server, then open a browser window for Linux Foundation SSO.

### Cursor

*Cursor requires a client ID. The following client ID only works with Cursor.*

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

If your MCP client is not listed here, you may try using [mcp-remote](https://github.com/geelen/mcp-remote) as a local proxy.

*The client ID shown here is only for embedding mcp-remote as a proxy and does not support direct MCP client use.*

The port `3334` is required so that the OAuth callback URL matches the registered client. Please refer to your client's documentation for the exact configuration syntax.

```json
{
  "mcpServers": {
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

A browser window will open for authentication on first use. To re-authenticate, clear cached credentials with:

```bash
rm -rf ~/.mcp-auth
```

### MCP Inspector (developer testing)

*MCP Inspector requires a client ID. The following client ID only works with MCP Inspector.*

[MCP Inspector](https://github.com/modelcontextprotocol/inspector) is a browser-based tool for exploring and testing MCP servers. To connect it to the LFX MCP Server:

```bash
npx @modelcontextprotocol/inspector --transport http --server-url https://mcp.lfx.dev/mcp
```

From the MCP Inspector sidebar, find **Authentication** → **OAuth 2.0 Flow** → **Client ID** and enter `4ibLLbnz9kwMEcE3RUCUH51F0RS3Hx3O`.

Hitting **Connect** will open a browser window for LFID login.

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

| Tool                              | Description                                                                     |
|-----------------------------------|---------------------------------------------------------------------------------|
| `search_members`                  | Search and filter members (memberships) by status, tier, organization, and more |
| `get_member_membership`           | Get a single member's membership details by member and membership ID            |
| `list_project_tiers`              | List all membership tiers (e.g. Gold, Silver, Bronze) defined for a project     |
| `get_project_tier`                | Get a single membership tier by project and tier UID                            |
| `get_membership_key_contacts`     | Get key contacts (primary contacts, board members) for a membership             |
| `get_membership_key_contact`      | Get a single key contact by project, membership, and contact UID                |
| `create_membership_key_contact`   | Add a key contact to a membership                                               |
| `update_membership_key_contact`   | Update an existing key contact on a membership                                  |
| `delete_membership_key_contact`   | Remove a key contact from a membership                                          |

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
| `search_past_meetings`             | Search past meetings; filter by project, committee, date range          |
| `get_past_meeting`                 | Get a past meeting by UID                                               |
| `search_past_meeting_participants` | Search past meeting participants; filter by meeting, committee, project |
| `get_past_meeting_participant`     | Get a past meeting participant by UID                                   |
| `search_past_meeting_summaries`    | Search past meeting summaries; filter by meeting, committee, project    |
| `get_past_meeting_summary`         | Get a past meeting summary by UID                                       |

### Discord

| Tool                    | Description                                                  |
|-------------------------|--------------------------------------------------------------|
| `list_discord_roles`    | List all roles in a project's Discord guild                  |
| `find_discord_role`     | Find a Discord role by name                                  |
| `find_discord_user`     | Find a Discord guild member by name and optional email       |
| `check_discord_user_role` | Check whether a Discord user already has a specific role   |
| `assign_discord_role`   | Assign a Discord role to a user                              |

### Email

| Tool                    | Description                                                  |
|-------------------------|--------------------------------------------------------------|
| `list_email_templates`  | List all available email templates for a project             |
| `send_email`            | Send a templated email via LFX mail servers                  |

### LFX Lens

| Tool              | Description                                                                                         |
|-------------------|-----------------------------------------------------------------------------------------------------|
| `query_lfx_lens`  | Ask natural-language questions about a project's data (events, contributors, health, value, and more) |

### B2B Organizations

| Tool                       | Description                                                   |
|----------------------------|---------------------------------------------------------------|
| `search_b2b_orgs`          | Search and list B2B organizations                             |
| `list_b2b_org_memberships` | List all project memberships for a B2B organization           |

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
