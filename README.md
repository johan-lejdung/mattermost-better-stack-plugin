# Mattermost Better Stack Plugin

> **This is an unofficial, community-built plugin. It is not affiliated with, endorsed by, or supported by Better Stack or Mattermost, Inc.**

A Mattermost plugin that integrates [Better Stack](https://betterstack.com) incident alerts, on-call schedules, and monitor status directly into your Mattermost workspace.

## Notice

This plugin was built with the assistance of AI tooling and has been reviewed by the project maintainer. Users are encouraged to **review the source code themselves** before deploying it in their own environment.

## Features

- **Incident threads** — When Better Stack fires a webhook, a post is created in your configured alert channel. Each status update (acknowledged, resolved) is posted as a thread reply on the original incident post, keeping the full incident lifecycle in one place. The post footer is updated in-place to always show the latest status.
- **Surrounding logs** — If your Better Stack webhook payload includes `surrounding_logs`, they are posted as a separate thread reply rather than cluttering the main incident post.
- **On-call change notifications** — The plugin polls Better Stack every 10 minutes and posts to the alert channel whenever the on-call person changes, tagging them with a Mattermost `@mention` if their Better Stack email matches a Mattermost account.
- **Daily on-call digest** — Optionally post the full on-call roster every morning at 08:00 CET/CEST, regardless of whether anything has changed.
- **Slash commands** — Query Better Stack directly from any Mattermost channel:
    - `/betterstack oncall` — Who is currently on call, and next person on call across all schedules
    - `/betterstack incidents` — Active (unresolved) incidents
    - `/betterstack monitors all` — All monitors and their current status
    - `/betterstack monitors down` — Only failing monitors
    - `/betterstack status` — Status pages and their aggregate state
- **Webhook URL display** — The System Console settings page shows the full webhook URL ready to copy into Better Stack.

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

The **Webhook URL** is displayed at the top of the settings page once a token has been generated. Copy this URL and add it as a webhook destination in Better Stack.

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
            "response_content": "$RESPONSE_CONTENT",
            "response_url": "$RESPONSE_URL",
            "screenshot_url": "$SCREENSHOT_URL",
            "surrounding_logs": "$METADATA.Surrounding logs"
        }
    }
}
```

Configure the same webhook URL for all three Better Stack alert types: **alarm**, **acknowledged**, and **resolved**. The plugin will thread all updates onto the original incident post automatically.

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

MIT — see [LICENSE](LICENSE).
