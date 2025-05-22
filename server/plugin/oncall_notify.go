package plugin

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/mattermost/mattermost/server/public/model"
)

// centralEuropeanLocation is Europe/Stockholm which observes CET (UTC+1) in winter
// and CEST (UTC+2) in summer, switching automatically with DST.
var centralEuropeanLocation = func() *time.Location {
	loc, err := time.LoadLocation("Europe/Stockholm")
	if err != nil {
		// Fallback to a fixed UTC+1 offset if the timezone database is unavailable.
		// This should not happen in practice on Linux containers.
		return time.FixedZone("CET", 1*60*60)
	}
	return loc
}()

// checkOnCallChanges fetches the current on-call state from Better Stack,
// compares it against the last known state in the KV store, and posts to the
// alert channel whenever a rotation change is detected.
//
// Additionally, if the DailyOncallDigest setting is enabled and it is 08:00
// CEST (within the current 10-minute polling window), a full digest is posted
// regardless of whether anything changed.
func (p *BetterStackPlugin) checkOnCallChanges() {
	config := p.getConfiguration()

	if config.BetterStackUptimeAPIToken == "" {
		return
	}
	if config.AlertChannelID == "" {
		return
	}

	client := newBetterStackClient(config.BetterStackUptimeAPIToken)
	schedules, userDetails, err := client.GetOnCallSchedules()
	if err != nil {
		p.API.LogError("Failed to fetch on-call schedules for change detection", "error", err.Error())
		return
	}

	// Determine whether this is the daily digest window: 08:00–08:10 CEST.
	now := time.Now().In(centralEuropeanLocation)
	isDailyDigestWindow := config.DailyOncallDigest &&
		now.Hour() == 8 && now.Minute() < 10

	// If digest window: check if we already posted today.
	if isDailyDigestWindow {
		today := now.Format("2006-01-02")
		lastDate, err := p.kvstore.GetLastDigestDate()
		if err != nil {
			p.API.LogError("Failed to get last digest date", "error", err.Error())
		}
		if lastDate == today {
			// Already posted today's digest — don't post again.
			isDailyDigestWindow = false
		}
	}

	// Process each schedule: detect changes and optionally collect digest lines.
	var changedLines []string // schedules whose on-call user changed
	var digestLines []string  // all schedules for the daily digest

	for _, schedule := range schedules {
		// Build the current set of on-call user IDs (sorted for stable comparison).
		currentIDs := make([]string, 0, len(schedule.Relationships.OnCallUsers.Data))
		for _, u := range schedule.Relationships.OnCallUsers.Data {
			currentIDs = append(currentIDs, u.ID)
		}
		sort.Strings(currentIDs)

		// Load previous state.
		previousIDs, err := p.kvstore.GetOnCallState(schedule.ID)
		if err != nil {
			p.API.LogError("Failed to get on-call state", "schedule_id", schedule.ID, "error", err.Error())
			continue
		}

		changed := !stringSlicesEqual(previousIDs, currentIDs)

		// Persist updated state regardless.
		if err := p.kvstore.StoreOnCallState(schedule.ID, currentIDs); err != nil {
			p.API.LogError("Failed to store on-call state", "schedule_id", schedule.ID, "error", err.Error())
		}

		// If this is the very first run (previousIDs == nil), seed silently.
		if previousIDs == nil {
			continue
		}

		scheduleName := scheduleDisplayName(schedule)
		userLine := p.resolveOnCallUsers(schedule, userDetails)

		if isDailyDigestWindow {
			digestLines = append(digestLines, fmt.Sprintf("**%s** — %s", scheduleName, userLine))
		}

		if changed {
			changedLines = append(changedLines, fmt.Sprintf("**%s** — %s", scheduleName, userLine))
		}
	}

	// Post rotation change notification.
	if len(changedLines) > 0 {
		msg := ":pager: **On-call rotation changed**\n\n" + strings.Join(changedLines, "\n")
		p.postToAlertChannel(config.AlertChannelID, msg)
	}

	// Post daily digest (skip if we just posted a change notification covering the same info).
	if isDailyDigestWindow && len(digestLines) > 0 {
		today := time.Now().In(centralEuropeanLocation).Format("2006-01-02")

		msg := ":calendar: **On-call today**\n\n" + strings.Join(digestLines, "\n")
		p.postToAlertChannel(config.AlertChannelID, msg)

		if err := p.kvstore.StoreLastDigestDate(today); err != nil {
			p.API.LogError("Failed to store last digest date", "error", err.Error())
		}
	}
}

// resolveOnCallUsers builds a string listing the on-call users for a schedule,
// using Mattermost @mentions where possible and falling back to email otherwise.
func (p *BetterStackPlugin) resolveOnCallUsers(schedule OnCallSchedule, userDetails map[string]OnCallUserDetail) string {
	if len(schedule.Relationships.OnCallUsers.Data) == 0 {
		return "_nobody_"
	}

	mentions := make([]string, 0, len(schedule.Relationships.OnCallUsers.Data))
	for _, u := range schedule.Relationships.OnCallUsers.Data {
		detail, ok := userDetails[u.ID]
		if !ok {
			// Fallback to the email stored in the relationship meta.
			mentions = append(mentions, u.Meta.Email)
			continue
		}

		email := detail.Attributes.Email
		mmUser, appErr := p.API.GetUserByEmail(email)
		if appErr != nil || mmUser == nil {
			// Not in Mattermost — use email.
			mentions = append(mentions, email)
			continue
		}

		mentions = append(mentions, "@"+mmUser.Username)
	}

	return strings.Join(mentions, ", ")
}

// postToAlertChannel creates a post in the alert channel as the bot user.
func (p *BetterStackPlugin) postToAlertChannel(channelID, message string) {
	post := &model.Post{
		UserId:    p.botUserID,
		ChannelId: channelID,
		Message:   message,
	}
	if _, appErr := p.API.CreatePost(post); appErr != nil {
		p.API.LogError("Failed to post on-call notification", "error", appErr.Error())
	}
}

// scheduleDisplayName returns a human-readable name for the schedule.
func scheduleDisplayName(s OnCallSchedule) string {
	if s.Attributes.Name != "" {
		return s.Attributes.Name
	}
	if s.Attributes.TeamName != "" {
		return s.Attributes.TeamName
	}
	return "Schedule " + s.ID
}

// stringSlicesEqual returns true if two sorted string slices are identical.
func stringSlicesEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
