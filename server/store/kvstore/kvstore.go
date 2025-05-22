package kvstore

// KVStore defines the methods used to persist plugin data.
type KVStore interface {
	// StoreIncidentPost stores a mapping from a Better Stack incident ID to the
	// Mattermost post ID of the original alert post in the channel.
	StoreIncidentPost(incidentID string, postID string) error

	// GetIncidentPost retrieves the Mattermost post ID associated with the given
	// Better Stack incident ID. Returns an empty string if no mapping is found.
	GetIncidentPost(incidentID string) (string, error)

	// StoreOnCallState stores the current list of on-call user IDs for a given
	// Better Stack schedule ID.
	StoreOnCallState(scheduleID string, userIDs []string) error

	// GetOnCallState retrieves the last known list of on-call user IDs for a
	// given schedule ID. Returns nil (and no error) when no state is stored yet.
	GetOnCallState(scheduleID string) ([]string, error)

	// StoreLastDigestDate stores the date string (YYYY-MM-DD) of the last daily
	// digest post, used to ensure only one digest fires per day.
	StoreLastDigestDate(date string) error

	// GetLastDigestDate retrieves the date string of the last daily digest post.
	// Returns an empty string if no digest has been posted yet.
	GetLastDigestDate() (string, error)
}
