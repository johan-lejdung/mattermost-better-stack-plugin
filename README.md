# Mattermost Better Stack Plugin

> **This is an unofficial, community-built plugin. It is not affiliated with, endorsed by, or supported by Better Stack or Mattermost, Inc.**

A Mattermost plugin that integrates [Better Stack](https://betterstack.com) incident alerts, on-call schedules, and monitor status directly into your Mattermost workspace.

## Notice

This plugin was built with the assistance of AI tooling and has been reviewed by the project maintainer. Users are encouraged to **review the source code themselves** before deploying it in their own environment.

## Features

- **Incident threads** — When Better Stack fires a webhook, a post is created in your configured alert channel. Every subsequent event (acknowledged, resolved, reopened, commented) is posted as a thread reply on the original incident post, keeping the full incident lifecycle in one place. The post itself is re-rendered on each event, so it always shows the current status and who acknowledged or resolved the incident.
- **Surrounding logs** — If your Better Stack webhook payload includes `surrounding_logs`, they are posted as a separate thread reply rather than cluttering the main incident post.
- **On-call change notifications** — The plugin polls Better Stack every 10 minutes and posts to the alert channel whenever the on-call person changes, tagging them with a Mattermost `@mention` if their Better Stack email matches a Mattermost account.
- **Daily on-call digest** — Optionally post the full on-call roster every morning at 08:00 CET/CEST, regardless of whether anything has changed.
- **Slash commands** — Query Better Stack directly from any Mattermost channel:
    - `/betterstack oncall` — Who is currently on call, and next person on call across all schedules
    - `/betterstack incidents` — Active (unresolved) incidents
    - `/betterstack monitors all` — All monitors and their current status
    - `/betterstack monitors down` — Only failing monitors
    - `/betterstack status` — Status pages and their aggregate state
- **Uptime check endpoint** — A secret-token-protected health endpoint you can point a Better Stack HTTP monitor at, so you get alerted when the integration itself breaks (see [Uptime Check](#uptime-check)).
- **Webhook URL display** — The System Console settings page shows the full webhook URL and uptime check URL ready to copy into Better Stack.

## Requirements

- Mattermost Server v6.2.1 or later
- A Better Stack account with Uptime monitoring

## Installation

### From a release

1. Download the latest `.tar.gz` from the [Releases](https://github.com/johan-lejdung/mattermost-better-stack-plugin/releases) page.
2. In Mattermost, go to **System Console → Plugins → Plugin Management**.
3. Upload the `.tar.gz` file and enable the plugin.

### Build from source

Requires Go 1.21+, Node.js 18+, and Make.

```bash
git clone https://github.com/johan-lejdung/mattermost-better-stack-plugin
cd mattermost-better-stack-plugin
make dist
```

This produces `dist/com.mattermost.plugin-better-stack-<version>.tar.gz`. Upload that file via **System Console → Plugins → Plugin Management**.

## Configuration

Go to **System Console → Plugins → Better Stack** and configure the following fields:

| Field                             | Description                                                                                                                              |
| --------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------- |
| **Better Stack Uptime API Token** | Your Better Stack API token — found under **betterstack.com → Uptime → API**                                                             |
| **Alert Channel ID**              | The Mattermost channel ID where incident posts and on-call notifications are created. Find it via **Channel Settings → Copy Channel ID** |
| **Webhook Secret Token**          | Auto-generated — click **Generate** if empty, then copy the Webhook URL shown at the top of the settings page                            |
| **Webhook Basic Auth Username**   | Optional. If set (with a password), incoming webhooks must present HTTP Basic Auth credentials                                           |
| **Webhook Basic Auth Password**   | Optional. Must be set together with a username, or both left empty                                                                       |
| **Daily On-Call Digest**          | When enabled, posts the full on-call roster to the alert channel every morning at 08:00 CET/CEST (Europe/Stockholm)                      |

The **Webhook URL** and **Uptime Check URL** are displayed at the top of the settings page once a token has been generated. Copy the webhook URL and add it as a webhook destination in Better Stack; see [Uptime Check](#uptime-check) for the second one.

## Better Stack Webhook Payload

Configure your Better Stack webhook with the following JSON body to include surrounding logs in incident posts:

```json
{
    "data": {
        "id": "$INCIDENT_ID",
        "type": "incident",
        "attributes": {
            "name": "$NAME",
            "url": "$INCIDENT_URL",
            "http_method": "$HTTP_METHOD",
            "cause": "$CAUSE",
            "started_at": "$STARTED_AT",
            "acknowledged_at": "$ACKNOWLEDGED_AT",
            "acknowledged_by": "$ACKNOWLEDGED_BY",
            "resolved_at": "$RESOLVED_AT",
            "resolved_by": "$RESOLVED_BY",
            "response_content": "$RESPONSE_CONTENT",
            "response_url": "$RESPONSE_URL",
            "screenshot_url": "$SCREENSHOT_URL",
            "surrounding_logs": "$METADATA.Surrounding logs",
            "comment_id": "$COMMENT_ID",
            "comment_content": "$COMMENT_CONTENT",
            "comment_created_at": "$COMMENT_CREATED_AT",
            "comment_author_name": "$COMMENT_AUTHOR_NAME",
            "comment_author_email": "$COMMENT_AUTHOR_EMAIL"
        }
    }
}
```

**Use this same body for every event.** The plugin re-renders the incident post from each payload it receives, so a body that omits fields on some events will drop them from the post. Variables Better Stack cannot fill (`$RESOLVED_BY` on an alarm, the `$COMMENT_*` set on a status change) arrive unexpanded and are ignored, so a single body template is safe for all of them.

Configure the same webhook URL for every Better Stack incident event: **alarm**, **acknowledged**, **resolved**, **reopened**, and **commented**. The plugin threads all updates onto the original incident post automatically:

| Event         | What the plugin does                                                                                              |
| ------------- | ------------------------------------------------------------------------------------------------------------------ |
| **alarm**     | Creates the incident post, plus a thread reply with the surrounding logs if the payload includes them              |
| **acknowledged** | Thread reply `:bell: Acknowledged by <acknowledger>`, and updates the post's status and Acknowledged line       |
| **resolved**  | Thread reply `:white_check_mark: Resolved by <resolver>`, and updates the post's status and Resolved line          |
| **reopened**  | Thread reply `:arrows_counterclockwise: Reopened`, clears the stale acknowledgement and resolution from the post   |
| **commented** | Thread reply `:speech_balloon: Comment from <author>` quoting the comment; the incident status is left unchanged   |

A reopen is recognised by the incident alarming again after its post showed RESOLVED — Better Stack clears the acknowledged and resolved timestamps on reopen, so the two are otherwise indistinguishable.

## Uptime Check

The plugin exposes a health endpoint so you can be alerted when the integration itself stops working — a revoked API token, a deleted alert channel, or a plugin that is no longer running:

```
GET <siteUrl>/plugins/com.mattermost.plugin-better-stack/api/v1/health/<webhook-token>
```

The URL is shown under **Uptime Check URL** at the top of the System Console settings page. It is protected by the same secret token as the webhook (and the same optional Basic Auth), so it is not reachable by anyone who does not already hold the webhook URL — an unauthenticated request gets a bare `401` and reveals nothing about the instance.

The endpoint returns **200** when everything is healthy and **503** when any check fails, so a Better Stack HTTP monitor can alarm on the status code alone. The body names the failing check:

```json
{
    "status": "fail",
    "checks": [
        {"name": "bot_account", "status": "ok"},
        {"name": "alert_channel", "status": "ok"},
        {"name": "better_stack_api", "status": "fail", "detail": "Better Stack API returned 401: unauthorized"}
    ]
}
```

| Check              | Fails when                                                                                       |
| ------------------ | ------------------------------------------------------------------------------------------------ |
| `bot_account`      | The BetterStack bot account was not created, so the plugin cannot post anything                  |
| `alert_channel`    | No alert channel is configured, or the configured channel can no longer be read (e.g. deleted)   |
| `better_stack_api` | No API token is configured, or the Better Stack Uptime API rejects it / cannot be reached        |

If the plugin is disabled or the Mattermost server is down, the request fails outright — which is exactly what the monitor should alarm on.

The `better_stack_api` result is cached for 60 seconds, so polling the endpoint more frequently than that will not consume extra Better Stack API rate limit. `HEAD` requests are supported for monitors configured that way.

> **Note:** Point the monitor at this endpoint from a Better Stack account or an external monitoring service — monitoring the plugin from within the same Mattermost instance it reports on would not catch a full outage.

## On-Call Notifications

The plugin polls the Better Stack on-call API every 10 minutes. When the on-call person for any schedule changes, a message like the following is posted to the alert channel:

```
🔔 On-call rotation changed

**Production** — @alice
**Tier 2** — @bob
```

The plugin resolves Better Stack email addresses to Mattermost usernames automatically. If no matching Mattermost account is found, the email address is shown instead.

On first activation, the plugin seeds its internal state silently — no spurious "changed" notification is posted on deploy.

## Development

### Prerequisites

- Go 1.21+
- Node.js 18+
- Make

### Build & deploy to a local Mattermost instance

```bash
export MM_SERVICESETTINGS_SITEURL=http://localhost:8065
export MM_ADMIN_USERNAME=admin
export MM_ADMIN_PASSWORD=yourpassword
make deploy
```

### Build a release tarball

```bash
git tag v1.0.0
make dist
# Output: dist/com.mattermost.plugin-better-stack-1.0.0.tar.gz
```

## License

Apache License 2.0 — see [LICENSE](LICENSE).
