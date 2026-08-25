package services

import (
	"bbs-go/internal/models"
	"bbs-go/internal/models/constants"
	"bbs-go/internal/pkg/idcodec"
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
	forumTopicCreatedCommand    = "forum.topic.created"
	forumRealtimeFromUID        = "system"
	forumWatcherTTL             = 6 * time.Minute
	forumSubscriberBatch        = 500
	forumWatcherMaxWatchIDBytes = 160
	forumWatcherMaxLeasesPerUID = 8
)

type forumRealtimeService struct {
	mu           sync.Mutex
	watchers     map[int64]map[string]forumWatcherLease
	feedWatchers map[string]forumWatcherLease
	lastSweep    time.Time
	client       *http.Client
}

type forumWatcherLease struct {
	UID       string
	ExpiresAt time.Time
}

var ForumRealtimeService = &forumRealtimeService{
	watchers:     make(map[int64]map[string]forumWatcherLease),
	feedWatchers: make(map[string]forumWatcherLease),
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

// WatchFeed maintains a short-lived set of Android users that currently have
// a forum topic list visible. Topic-created events are only pushed to these
// users, so opening the community does not subscribe every account forever.
func (s *forumRealtimeService) WatchFeed(uid, watchID string, active bool) {
	uid = strings.TrimSpace(uid)
	watchID = strings.TrimSpace(watchID)
	if uid == "" {
		return
	}
	if watchID == "" {
		watchID = "default"
	}
	if len(watchID) > forumWatcherMaxWatchIDBytes {
		return
	}
	leaseKey := uid + "\x00" + watchID
	now := time.Now()
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sweepExpiredLocked(now)
	if !active {
		delete(s.feedWatchers, leaseKey)
		return
	}

	count := 0
	oldestKey := ""
	var oldestExpiry time.Time
	for key, lease := range s.feedWatchers {
		if lease.UID != uid {
			continue
		}
		count++
		if oldestKey == "" || lease.ExpiresAt.Before(oldestExpiry) {
			oldestKey = key
			oldestExpiry = lease.ExpiresAt
		}
	}
	if _, exists := s.feedWatchers[leaseKey]; !exists && count >= forumWatcherMaxLeasesPerUID && oldestKey != "" {
		delete(s.feedWatchers, oldestKey)
	}
	s.feedWatchers[leaseKey] = forumWatcherLease{UID: uid, ExpiresAt: now.Add(forumWatcherTTL)}
}

func (s *forumRealtimeService) activeFeedWatchers() []string {
	now := time.Now()
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sweepExpiredLocked(now)
	if len(s.feedWatchers) == 0 {
		return nil
	}
	ret := make([]string, 0, len(s.feedWatchers))
	seen := make(map[string]struct{}, len(s.feedWatchers))
	for key, lease := range s.feedWatchers {
		if !lease.ExpiresAt.After(now) {
			delete(s.feedWatchers, key)
			continue
		}
		if _, ok := seen[lease.UID]; ok {
			continue
		}
		seen[lease.UID] = struct{}{}
		ret = append(ret, lease.UID)
	}
	return ret
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
	for key, lease := range s.feedWatchers {
		if !lease.ExpiresAt.After(now) {
			delete(s.feedWatchers, key)
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

	s.sendBatches(baseURL, fmt.Sprintf("forum_topic_%d", topicID), subscribers, payload, topicID)
}

func (s *forumRealtimeService) sendBatches(baseURL, channelID string, subscribers []string, payload []byte, topicID int64) {
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
			if err := s.send(baseURL, channelID, batch, payload); err != nil {
				attrs := []any{slog.String("channel_id", channelID), slog.Int("subscribers", len(batch)), slog.Any("err", err)}
				if topicID > 0 {
					attrs = append(attrs, slog.Int64("topic_id", topicID))
				}
				slog.Warn("forum realtime cmd send failed", attrs...)
			}
		}(batch)
	}
	wg.Wait()
}

// PushTopicCreated sends a lightweight "new topic available" hint to active
// Android topic-list viewers. The client refreshes REST on tap; the CMD does not
// carry trusted topic content and therefore never replaces the database API.
func (s *forumRealtimeService) PushTopicCreated(topic *models.Topic) {
	if topic == nil || topic.Id <= 0 || topic.Status != constants.StatusOk {
		return
	}
	subscribers := s.activeFeedWatchers()
	if len(subscribers) == 0 {
		return
	}
	baseURL := strings.TrimRight(strings.TrimSpace(os.Getenv("BBSGO_WUKONGIM_API_URL")), "/")
	if baseURL == "" {
		return
	}
	tags := TopicService.GetTopicTags(topic.Id)
	tagIDs := make([]int64, 0, len(tags))
	for _, tag := range tags {
		if tag.Id > 0 {
			tagIDs = append(tagIDs, tag.Id)
		}
	}
	payload, err := json.Marshal(map[string]any{
		"cmd":  forumTopicCreatedCommand,
		"type": 99,
		"param": map[string]any{
			"topic_id":    idcodec.Encode(topic.Id),
			"category_id": topic.CategoryId,
			"tag_ids":     tagIDs,
			"create_time": topic.CreateTime,
		},
	})
	if err != nil {
		slog.Warn("forum topic realtime payload marshal failed", slog.Any("err", err))
		return
	}
	s.sendBatches(baseURL, "forum_topic_feed", subscribers, payload, 0)
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

func (s *forumRealtimeService) send(baseURL, channelID string, subscribers []string, payload []byte) error {
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
		ChannelID:   channelID,
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
	// WuKongIM v1/v2 deployments can enable managerToken. In that mode the
	// HTTP API requires the token request header; newer/internal-only setups may
	// leave it empty. Supporting it optionally keeps forum realtime compatible
	// with both configurations without exposing the token to clients.
	if managerToken := strings.TrimSpace(os.Getenv("BBSGO_WUKONGIM_MANAGER_TOKEN")); managerToken != "" {
		req.Header.Set("token", managerToken)
	}
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
