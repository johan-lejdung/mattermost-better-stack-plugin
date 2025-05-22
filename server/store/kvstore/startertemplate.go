package kvstore

import (
	"github.com/mattermost/mattermost/server/public/pluginapi"
	"github.com/pkg/errors"
)

const (
	incidentPostKeyPrefix = "betterstack_incident_"
	onCallStateKeyPrefix  = "betterstack_oncall_"
	lastDigestDateKey     = "betterstack_last_digest_date"
)

// Client wraps the pluginapi KV store methods to provide typed access.
type Client struct {
	client *pluginapi.Client
}

func NewKVStore(client *pluginapi.Client) KVStore {
	return Client{
		client: client,
	}
}

// StoreIncidentPost saves the incidentID → postID mapping in the KV store.
func (kv Client) StoreIncidentPost(incidentID string, postID string) error {
	ok, err := kv.client.KV.Set(incidentPostKeyPrefix+incidentID, postID)
	if err != nil {
		return errors.Wrap(err, "failed to store incident post mapping")
	}
	if !ok {
		return errors.New("KV store Set returned false for incident post mapping")
	}
	return nil
}

// GetIncidentPost retrieves the Mattermost post ID for the given Better Stack incident ID.
// Returns an empty string (and no error) when no mapping exists.
func (kv Client) GetIncidentPost(incidentID string) (string, error) {
	var postID string
	err := kv.client.KV.Get(incidentPostKeyPrefix+incidentID, &postID)
	if err != nil {
		return "", errors.Wrap(err, "failed to get incident post mapping")
	}
	return postID, nil
}

// StoreOnCallState saves the current on-call user ID list for a schedule.
func (kv Client) StoreOnCallState(scheduleID string, userIDs []string) error {
	ok, err := kv.client.KV.Set(onCallStateKeyPrefix+scheduleID, userIDs)
	if err != nil {
		return errors.Wrap(err, "failed to store on-call state")
	}
	if !ok {
		return errors.New("KV store Set returned false for on-call state")
	}
	return nil
}

// GetOnCallState retrieves the last known on-call user ID list for a schedule.
// Returns nil (and no error) when no state has been stored yet.
func (kv Client) GetOnCallState(scheduleID string) ([]string, error) {
	var userIDs []string
	err := kv.client.KV.Get(onCallStateKeyPrefix+scheduleID, &userIDs)
	if err != nil {
		return nil, errors.Wrap(err, "failed to get on-call state")
	}
	return userIDs, nil
}

// StoreLastDigestDate saves the date (YYYY-MM-DD) of the last daily digest post.
func (kv Client) StoreLastDigestDate(date string) error {
	ok, err := kv.client.KV.Set(lastDigestDateKey, date)
	if err != nil {
		return errors.Wrap(err, "failed to store last digest date")
	}
	if !ok {
		return errors.New("KV store Set returned false for last digest date")
	}
	return nil
}

// GetLastDigestDate retrieves the date of the last daily digest post.
// Returns an empty string (and no error) when none has been posted yet.
func (kv Client) GetLastDigestDate() (string, error) {
	var date string
	err := kv.client.KV.Get(lastDigestDateKey, &date)
	if err != nil {
		return "", errors.Wrap(err, "failed to get last digest date")
	}
	return date, nil
}
