# LFX MCP Server

A [Model Context Protocol](https://modelcontextprotocol.io/) (MCP) server that connects AI assistants to the Linux Foundation's LFX platform.

## What You Can Do

- **Explore projects** — Search and retrieve details for any LFX project
- **Manage committees** — Search, create, update, and delete committees and their members across projects
- **Work with mailing lists** — Search mailing lists and their subscribers
- **Query members** — Search memberships by tier, status, organization, and more; get key contacts
- **Track meetings** — Find upcoming meetings, registrants, past participants, transcripts, and AI-generated summaries

## Quick Start

### Prerequisites

- Go 1.26.0+

### Build & Run

```bash
git clone https://github.com/linuxfoundation/lfx-mcp.git
cd lfx-mcp
make build
```

**Stdio transport** (default — used by Claude Desktop, Claude Code, etc.):

```bash
./bin/lfx-mcp-server
```

**HTTP transport** (streamable HTTP with SSE):

```bash
./bin/lfx-mcp-server -mode=http
```

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
| `create_committee` | Create a new committee under a project |
| `update_committee` | Update a committee's base information |
| `update_committee_settings` | Update a committee's settings (visibility, email requirements, meeting attendee defaults) |
| `delete_committee` | Delete a committee by UID |
| `search_committee_members` | Search committee members; filter by committee, project, or name |
| `get_committee_member` | Get a specific committee member by committee and member UID |
| `create_committee_member` | Add a new member to a committee |
| `update_committee_member` | Update an existing committee member's information |
| `delete_committee_member` | Remove a member from a committee |

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

## Configuration

<details>
<summary>All flags and environment variables</summary>

Environment variables use the `LFXMCP_` prefix and **override** their corresponding flags.

| Flag | Env Var | Default | Description |
|------|---------|---------|-------------|
| `-mode` | `LFXMCP_MODE` | `stdio` | Transport mode: `stdio` or `http` |
| `-http.host` | `LFXMCP_HTTP_HOST` | `127.0.0.1` | HTTP server bind address |
| `-http.port` | `LFXMCP_HTTP_PORT` | `8080` | HTTP server port |
| `-http.public_url` | `LFXMCP_HTTP_PUBLIC_URL` | — | Public URL for HTTP transport (reverse proxies) |
| `-debug` | `LFXMCP_DEBUG` | `false` | Enable debug logging with source locations |
| `-debug_traffic` | `LFXMCP_DEBUG_TRAFFIC` | `false` | Log outbound LFX API request/response bodies |
| `-tools` | `LFXMCP_TOOLS` | — | Comma-separated list of tools to enable |
| `-mcp_api.auth_servers` | `LFXMCP_MCP_API_AUTH_SERVERS` | — | OAuth authorization server URLs (comma-separated) |
| `-mcp_api.public_url` | `LFXMCP_MCP_API_PUBLIC_URL` | — | Public URL for MCP API (OAuth PRM) |
| `-mcp_api.scopes` | `LFXMCP_MCP_API_SCOPES` | — | OAuth scopes (comma-separated) |
| `-client_id` | `LFXMCP_CLIENT_ID` | — | OAuth client ID for token exchange |
| `-client_secret` | `LFXMCP_CLIENT_SECRET` | — | OAuth client secret |
| `-client_assertion_signing_key` | `LFXMCP_CLIENT_ASSERTION_SIGNING_KEY` | — | PEM-encoded RSA private key for client assertion |
| `-token_endpoint` | `LFXMCP_TOKEN_ENDPOINT` | — | OAuth2 token endpoint URL (RFC 8693) |
| `-lfx_api_url` | `LFXMCP_LFX_API_URL` | — | LFX API base URL (token exchange audience) |

</details>

## Development

See [AGENTS.md](AGENTS.md) for architecture details, adding tools, testing patterns, and coding guidelines.

```bash
make build    # Compile the binary
make check    # Format, vet, and lint
make test     # Run tests
```

## License

Copyright The Linux Foundation and each contributor to LFX.

This project's source code is licensed under the MIT License. A copy of the
license is available in LICENSE.

This project's documentation is licensed under the Creative Commons Attribution
4.0 International License \(CC-BY-4.0\). A copy of the license is available in
LICENSE-docs.
