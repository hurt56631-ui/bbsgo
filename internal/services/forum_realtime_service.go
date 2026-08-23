package services

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"
)

const (
	forumRealtimeCommand        = "forum.comment.created"
	forumRealtimeFromUID        = "system"
	forumWatcherTTL             = 6 * time.Minute
	forumSubscriberBatch        = 500
	forumWatcherMaxWatchIDBytes = 160
	forumWatcherMaxLeasesPerUID = 8
)

type forumRealtimeService struct {
	mu        sync.Mutex
	watchers  map[int64]map[string]forumWatcherLease
	lastSweep time.Time
	client    *http.Client
}

type forumWatcherLease struct {
	UID       string
	ExpiresAt time.Time
}

var ForumRealtimeService = &forumRealtimeService{
	watchers: make(map[int64]map[string]forumWatcherLease),
	client: &http.Client{
		Timeout: 2 * time.Second,
	},
}

// PublishComment forwards saved topic comments to active Android viewers through
// the existing WuKongIM connection. Web pages intentionally stay REST-only.
func (s *forumRealtimeService) PublishComment(entityType string, entityID int64, payload any) {
	if strings.EqualFold(strings.TrimSpace(entityType), "topic") {
		s.PushTopicComment(entityID, payload)
	}
}

// WatchTopic maintains a short-lived set of Talkami/WuKongIM uids that are
// actively viewing a topic. Android refreshes the lease while the Activity is
// visible. A missed unregister therefore cleans itself up automatically.
func (s *forumRealtimeService) WatchTopic(topicID int64, uid, watchID string, active bool) {
	uid = strings.TrimSpace(uid)
	watchID = strings.TrimSpace(watchID)
	if topicID <= 0 || uid == "" {
		return
	}

	if watchID == "" {
		watchID = "default"
	}
	// watchID comes from the client and is retained in memory for the lease TTL.
	// Bound it so an authenticated client cannot turn this lightweight realtime
	// registry into an unbounded memory sink. Normal Android activity IDs are
	// well below this limit.
	if len(watchID) > forumWatcherMaxWatchIDBytes {
		return
	}
	leaseKey := uid + "\x00" + watchID

	now := time.Now()
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sweepExpiredLocked(now)
	bucket := s.watchers[topicID]
	if !active {
		if bucket != nil {
			delete(bucket, leaseKey)
			if len(bucket) == 0 {
				delete(s.watchers, topicID)
			}
		}
		return
	}
	if bucket == nil {
		bucket = make(map[string]forumWatcherLease)
		s.watchers[topicID] = bucket
	}
	for key, lease := range bucket {
		if !lease.ExpiresAt.After(now) {
			delete(bucket, key)
		}
	}
	if _, alreadyPresent := bucket[leaseKey]; !alreadyPresent {
		count := 0
		oldestKey := ""
		var oldestExpiry time.Time
		for key, lease := range bucket {
			if lease.UID != uid {
				continue
			}
			count++
			if oldestKey == "" || lease.ExpiresAt.Before(oldestExpiry) {
				oldestKey = key
				oldestExpiry = lease.ExpiresAt
			}
		}
		if count >= forumWatcherMaxLeasesPerUID && oldestKey != "" {
			delete(bucket, oldestKey)
		}
	}
	bucket[leaseKey] = forumWatcherLease{UID: uid, ExpiresAt: now.Add(forumWatcherTTL)}
}

// ForgetTopic drops all in-memory viewing leases for a topic that has been
// physically deleted. New watch attempts are rejected by the API once the row
// no longer exists.
func (s *forumRealtimeService) ForgetTopic(topicID int64) {
	if topicID <= 0 {
		return
	}
	s.mu.Lock()
	delete(s.watchers, topicID)
	s.mu.Unlock()
}

func (s *forumRealtimeService) sweepExpiredLocked(now time.Time) {
	if !s.lastSweep.IsZero() && now.Sub(s.lastSweep) < time.Minute {
		return
	}
	for topicID, bucket := range s.watchers {
		for key, lease := range bucket {
			if !lease.ExpiresAt.After(now) {
				delete(bucket, key)
			}
		}
		if len(bucket) == 0 {
			delete(s.watchers, topicID)
		}
	}
	s.lastSweep = now
}

func (s *forumRealtimeService) activeWatchers(topicID int64) []string {
	if topicID <= 0 {
		return nil
	}
	now := time.Now()
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sweepExpiredLocked(now)
	bucket := s.watchers[topicID]
	if len(bucket) == 0 {
		return nil
	}
	ret := make([]string, 0, len(bucket))
	seenUIDs := make(map[string]struct{}, len(bucket))
	for key, lease := range bucket {
		if !lease.ExpiresAt.After(now) {
			delete(bucket, key)
			continue
		}
		if _, exists := seenUIDs[lease.UID]; exists {
			continue
		}
		seenUIDs[lease.UID] = struct{}{}
		ret = append(ret, lease.UID)
	}
	if len(bucket) == 0 {
		delete(s.watchers, topicID)
	}
	return ret
}

// PushTopicComment sends an ephemeral CMD through the existing WuKongIM
// connection. It is deliberately non-persistent and does not update the recent
// conversation list; the database/REST APIs remain the source of truth.
func (s *forumRealtimeService) PushTopicComment(topicID int64, param any) {
	subscribers := s.activeWatchers(topicID)
	if len(subscribers) == 0 {
		return
	}
	baseURL := strings.TrimRight(strings.TrimSpace(os.Getenv("BBSGO_WUKONGIM_API_URL")), "/")
	if baseURL == "" {
		return
	}

	payload, err := json.Marshal(map[string]any{
		"cmd":   forumRealtimeCommand,
		"type":  99,
		"param": param,
	})
	if err != nil {
		slog.Warn("forum realtime payload marshal failed", slog.Any("err", err))
		return
	}

	// Large classrooms can have thousands of simultaneous viewers. Send subscriber
	// batches with bounded concurrency so one hot topic does not serialize many
	// internal HTTP round trips, while still protecting WuKongIM from a burst.
	const maxConcurrentBatches = 4
	sem := make(chan struct{}, maxConcurrentBatches)
	var wg sync.WaitGroup
	for start := 0; start < len(subscribers); start += forumSubscriberBatch {
		end := start + forumSubscriberBatch
		if end > len(subscribers) {
			end = len(subscribers)
		}
		batch := append([]string(nil), subscribers[start:end]...)
		wg.Add(1)
		sem <- struct{}{}
		go func(batch []string) {
			defer wg.Done()
			defer func() { <-sem }()
			if err := s.send(baseURL, topicID, batch, payload); err != nil {
				slog.Warn("forum realtime cmd send failed",
					slog.Int64("topic_id", topicID), slog.Int("subscribers", len(batch)), slog.Any("err", err))
			}
		}(batch)
	}
	wg.Wait()
}

type forumIMHeader struct {
	NoPersist int `json:"no_persist"`
	RedDot    int `json:"red_dot"`
	SyncOnce  int `json:"sync_once"`
}

type forumIMSendRequest struct {
	Header      forumIMHeader `json:"header"`
	Setting     uint8         `json:"setting"`
	FromUID     string        `json:"from_uid"`
	ChannelID   string        `json:"channel_id"`
	ChannelType uint8         `json:"channel_type"`
	Subscribers []string      `json:"subscribers"`
	Payload     []byte        `json:"payload"`
}

func (s *forumRealtimeService) send(baseURL string, topicID int64, subscribers []string, payload []byte) error {
	body, err := json.Marshal(forumIMSendRequest{
		Header: forumIMHeader{
			NoPersist: 1,
			RedDot:    0,
			SyncOnce:  1,
		},
		// WuKongIM setting bit 6 = NoUpdateConversation. Forum activity events
		// therefore never create or reorder a chat conversation.
		Setting:     64,
		FromUID:     forumRealtimeFromUID,
		ChannelID:   fmt.Sprintf("forum_topic_%d", topicID),
		ChannelType: 2,
		Subscribers: subscribers,
		Payload:     payload,
	})
	if err != nil {
		return err
	}
	req, err := http.NewRequest(http.MethodPost, baseURL+"/message/send", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := s.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1024))
		return nil
	}
	msg, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	return fmt.Errorf("wukongim http %d: %s", resp.StatusCode, strings.TrimSpace(string(msg)))
}
