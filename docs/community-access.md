# Using the LFX MCP Server as a community member

## What it is

The LFX MCP Server lets an AI assistant you already use (Claude, Cursor, Goose, GitHub Copilot and others)
interact with LFX Self Serve functionality on your behalf, with your own LFX login: projects, groups, meetings,
membership, and the actions your role allows.

## Who can request access

Access is for people who take part in a Linux Foundation project or foundation that uses LFX Self Serve, whether
as a member or manager of a group (a committee, working group, TSC or board), a meeting host or guest, a project
or foundation administrator, or an organization admin. Through the MCP you see and do what LFX Self Serve lets
you see and do.

## How to request

Open a new issue in this repository with the
[LFX MCP access request](https://github.com/linuxfoundation/lfx-mcp/issues/new?template=access_request.yml)
form. It asks whether to grant or remove access, the LFID username, and what you plan to do with the assistant;
the rest is optional.

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

## Appendix: sponsoring and removing access

For chairs, administrators and executive directors. We may ask someone in the requester's group to confirm their
participation; do that by commenting on the issue, or open the form on the person's behalf. To remove someone's
access, open the form and choose **Remove access**.
