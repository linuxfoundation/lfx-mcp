# Using the LFX MCP Server as a community member

## What it is

The LFX MCP Server lets an AI assistant you already use (Claude, Cursor, Goose, OpenCode, Zed, ChatGPT and others)
interact with LFX Self Serve functionality on your behalf, with your own LFX login: projects, committees, meetings,
membership, and the actions your role allows. See the [README](../README.md#connecting-to-the-lfx-mcp-server) for
the current list of supported clients; if yours is not listed, say so on the access request below and we will
follow up.

## Who can request access

This form is for project leadership: staff, board members, technical leads, or others in the community with
established operational responsibilities. It is also for organization leadership: a key contact (one of the named
contacts on a project's membership agreement) or an OSPO administrator, requesting access to their organization's
own data.

## What you can see and do

If your request is approved, Linux Foundation staff will grant you the appropriate permission within the LFX
Platform itself, as well as add you to the allowlist of community users allowed to interact with the LFX Platform
via our MCP server. The LFX MCP Server works like the GitHub MCP server: it lets your AI agent connect using the
level of access you already have, rather than granting anything new on its own. Pick the access level (read,
write, or revoke) and scope (entire project, one committee, or organization) closest to what you need.

Seeing data from other projects and organizations besides your own is expected and is not a bug: anyone signed in
sees public records across all of LFX, the same information published at
[the LFX Project Landscape](https://landscape.linuxfoundation.org/?group=projects) and elsewhere — public project
calendars, meeting recordings set to public by their organizers, and the public-record names of a project's staff and
governance bodies. If you believe you are seeing something that was marked public by mistake, report it through
the [LF Help Center chatbot](https://helpcenter.linuxfoundation.org/en/articles/9798558-introducing-the-lf-service-desk-chatbot)
rather than treating it as an MCP access problem.

**You can, based on your access level:**

- find projects, and the committees, meetings and mailing lists you take part in or have a role on;
- look up committee members, meeting registrants and attendees, under the same access rules as LFX Self Serve;
- get past meeting summaries, and links to recordings and transcripts shared with you;
- get counts and overviews across the projects, committees and meetings you can see;
- see what other projects, committees and meetings have made public, including those in other foundations, as
  anyone signed in to LFX Self Serve can;
- manage a committee and its members, or create and manage committees across a project;
- send emails from a project's templates and give people roles on its Discord server, where the project has
  these set up (these two are MCP features that LFX Self Serve does not have);
- see an organization's memberships, key contacts and committee seats.

**You cannot:**

- see a private committee, meeting, mailing list, or organization's membership data unless you take part in it,
  or have been granted explicit access to the project or committee it belongs to;
- access LFX Insights or other Linux Foundation analytics and reporting data;
- make changes your LFX roles do not allow.

If you sign in with a Linux Foundation staff account, you will also see internal reporting and data tools. They
are not available to non-staff community accounts.

## How to request

Open a new issue in this repository with the
[LFX MCP access request](https://github.com/linuxfoundation/lfx-mcp/issues/new?template=access_request.yml)
form. It asks what you're requesting (read, write, or a removal), the scope and location it applies to, the LFID
username, and what you plan to do with the assistant; the rest is optional.

**This repository, and the issue you file, are public.** Do not include passwords, tokens, or any private or
confidential project, meeting or member information in the request — a public name, your LFID, and a general
description of your intended use are all that is needed.

## What happens next

We reply on the issue and close it once the change is done. Then connect your client as below and sign in with
the same LFID.

## Connecting your client (personal accounts)

**Claude Desktop.** In the sidebar choose **Customize**, then **Connectors**. Use **+** and
**Add Custom Connector**, enter **LFX** and the URL `https://mcp.lfx.dev/mcp`, press **Add**, then **Connect**
and sign in with your LFID.

**Claude Code.** Run the command below, then `/mcp` inside Claude Code, select **lfx** and **Authenticate**.
A browser window opens for the LFID login.

```bash
claude mcp add --transport http lfx https://mcp.lfx.dev/mcp
```

**Other clients.** Cursor, Visual Studio Code, Goose, OpenCode, Zed, ChatGPT and more are covered in the
[README](../README.md#connecting-to-the-lfx-mcp-server). If you use the Linux Foundation's enterprise Claude, the
connector is already set up; see [Claude (LF enterprise)](../README.md#claude-lf-enterprise) to connect.

If sign-in is refused, the request may not be complete yet or you signed in with a different account; comment on
your issue.

## Good to know

- Your assistant acts as you. Do not share your login or session with other people or with shared bots.
- Transcripts and summaries contain what participants said. Treat them as you would the recording, under your
  foundation's rules and the Linux Foundation
  [privacy policy](https://www.linuxfoundation.org/legal/privacy-policy).

## Need help?

Comment on your issue. For account problems, use
[support.linuxfoundation.org](https://support.linuxfoundation.org). Please do not post access requests in Slack.
