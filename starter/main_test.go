package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
)

func resetStore() {
	store = &Store{
		stats:    make(map[string]*CampaignStats),
		seen:     make(map[string]Event),
		openedBy: make(map[string]map[string]bool),
	}
}

func TestMixedBatchDuplicateAndInvalid(t *testing.T) {
	resetStore()
	body := []byte(`[
		{"event_id":"e1","campaign_id":"c1","contact_id":"ct1","type":"opened","timestamp":"2026-08-10T09:15:04Z"},
		{"event_id":"e2","campaign_id":"c1","contact_id":"ct1","type":"opened","timestamp":"2026-08-10T09:16:04Z"},
		{"event_id":"e1","campaign_id":"c1","contact_id":"ct1","type":"opened","timestamp":"2026-08-10T09:15:04Z"},
		{"event_id":"bad","campaign_id":"c1","contact_id":"ct1","type":"opened","timestamp":"not-a-time"}
	]`)
	req := httptest.NewRequest(http.MethodPost, "/events", bytes.NewReader(body))
	res := httptest.NewRecorder()
	handlePostEvents(res, req)
	if res.Code != http.StatusMultiStatus {
		t.Fatalf("status = %d, want 207; body=%s", res.Code, res.Body.String())
	}

	statsReq := httptest.NewRequest(http.MethodGet, "/campaigns/c1/stats", nil)
	statsReq.SetPathValue("campaignID", "c1")
	statsRes := httptest.NewRecorder()
	handleGetStats(statsRes, statsReq)
	var stats CampaignStats
	if err := json.Unmarshal(statsRes.Body.Bytes(), &stats); err != nil {
		t.Fatal(err)
	}
	if stats.Opened != 2 || stats.UniqueOpens != 1 {
		t.Fatalf("opened=%d unique=%d, want opened=2 unique=1", stats.Opened, stats.UniqueOpens)
	}
}

func TestUniqueOpensArePerCampaign(t *testing.T) {
	resetStore()
	store.accept(Event{EventID: "a", CampaignID: "c1", ContactID: "ct1", Type: "opened", Timestamp: "2026-08-10T09:00:00Z"})
	store.accept(Event{EventID: "b", CampaignID: "c2", ContactID: "ct1", Type: "opened", Timestamp: "2026-08-10T09:00:00Z"})
	store.accept(Event{EventID: "c", CampaignID: "c1", ContactID: "ct1", Type: "opened", Timestamp: "2026-08-10T10:00:00Z"})

	c1, ok := store.statsFor("c1")
	if !ok || c1.Opened != 2 || c1.UniqueOpens != 1 {
		t.Fatalf("c1 = %+v, ok=%v", c1, ok)
	}
	c2, ok := store.statsFor("c2")
	if !ok || c2.UniqueOpens != 1 {
		t.Fatalf("c2 = %+v, ok=%v", c2, ok)
	}
}

func TestConflictingEventIDIsRejected(t *testing.T) {
	resetStore()
	first := Event{EventID: "conflict", CampaignID: "c1", ContactID: "ct1", Type: "sent", Timestamp: "2026-08-10T09:00:00Z"}
	second := Event{EventID: "conflict", CampaignID: "c1", ContactID: "ct1", Type: "delivered", Timestamp: "2026-08-10T09:00:00Z"}
	if status, _ := store.accept(first); status != "accepted" {
		t.Fatalf("first status=%q", status)
	}
	if status, conflict := store.accept(second); status != "rejected" || conflict == "" {
		t.Fatalf("second status=%q conflict=%q", status, conflict)
	}
	stats, _ := store.statsFor("c1")
	if stats.Sent != 1 || stats.Delivered != 0 {
		t.Fatalf("stats=%+v", stats)
	}
}

func TestConcurrentDuplicatePostsCountOnce(t *testing.T) {
	resetStore()
	body := []byte(`[{"event_id":"concurrent-1","campaign_id":"c1","contact_id":"ct1","type":"sent","timestamp":"2026-08-10T09:00:00Z"}]`)

	const requests = 50
	var wg sync.WaitGroup
	accepted := make(chan int, requests)
	duplicates := make(chan int, requests)
	for range requests {
		wg.Add(1)
		go func() {
			defer wg.Done()
			req := httptest.NewRequest(http.MethodPost, "/events", bytes.NewReader(body))
			res := httptest.NewRecorder()
			handlePostEvents(res, req)
			var result struct {
				Duplicates int `json:"duplicates"`
				Accepted   int `json:"accepted"`
			}
			if err := json.Unmarshal(res.Body.Bytes(), &result); err == nil {
				duplicates <- result.Duplicates
				accepted <- result.Accepted
			}
		}()
	}
	wg.Wait()
	close(accepted)
	close(duplicates)

	acceptedCount := 0
	duplicateResponses := 0
	for count := range accepted {
		acceptedCount += count
	}
	for count := range duplicates {
		duplicateResponses += count
	}
	if acceptedCount != 1 || duplicateResponses != requests-1 {
		t.Fatalf("accepted=%d duplicate responses=%d, want 1 and %d", acceptedCount, duplicateResponses, requests-1)
	}

	stats, ok := store.statsFor("c1")
	if !ok || stats.Sent != 1 {
		t.Fatalf("stats=%+v, ok=%v; sent must equal 1", stats, ok)
	}
}

func TestMalformedAndTrailingJSONRejected(t *testing.T) {
	resetStore()
	tests := []struct {
		name string
		body string
	}{
		{name: "malformed array", body: `[{"event_id":"bad"`},
		{name: "trailing object", body: `[] {}`},
		{name: "trailing second array", body: `[] []`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/events", strings.NewReader(tt.body))
			res := httptest.NewRecorder()
			handlePostEvents(res, req)
			if res.Code != http.StatusBadRequest {
				t.Fatalf("status=%d, want 400; body=%s", res.Code, res.Body.String())
			}
		})
	}
}

func TestRequestBodyMaximum(t *testing.T) {
	resetStore()
	req := httptest.NewRequest(http.MethodPost, "/events", strings.NewReader(strings.Repeat(" ", maxRequestBodyBytes+1)))
	res := httptest.NewRecorder()
	handlePostEvents(res, req)
	if res.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status=%d, want 413; body=%s", res.Code, res.Body.String())
	}
}

func TestTimestampAcceptsOffsetsAndNormalizesToUTC(t *testing.T) {
	event := Event{
		EventID:    "timestamp-offset",
		CampaignID: "c1",
		ContactID:  "ct1",
		Type:       "sent",
		Timestamp:  "2026-08-10T11:15:04+02:00",
	}
	if err := validateEvent(&event); err != nil {
		t.Fatalf("validateEvent returned %v", err)
	}
	if event.Timestamp != "2026-08-10T09:15:04Z" {
		t.Fatalf("timestamp=%q, want UTC timestamp", event.Timestamp)
	}

	resetStore()
	if status, _ := store.accept(event); status != "accepted" {
		t.Fatalf("first status=%q", status)
	}
	utcDuplicate := event
	utcDuplicate.Timestamp = "2026-08-10T09:15:04Z"
	if status, _ := store.accept(utcDuplicate); status != "duplicate" {
		t.Fatalf("UTC equivalent status=%q, want duplicate", status)
	}
}

func TestSeedDatasetCounts(t *testing.T) {
	resetStore()
	seed, err := os.ReadFile("seed/events.json")
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/events", bytes.NewReader(seed))
	res := httptest.NewRecorder()
	handlePostEvents(res, req)

	if res.Code != http.StatusMultiStatus {
		t.Fatalf("status=%d, want 207; body=%s", res.Code, res.Body.String())
	}
	var response struct {
		Accepted   int `json:"accepted"`
		Duplicates int `json:"duplicates"`
		Rejected   int `json:"rejected"`
	}
	if err := json.Unmarshal(res.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response.Accepted != 210 || response.Duplicates != 15 || response.Rejected != 10 {
		t.Fatalf("accepted=%d duplicates=%d rejected=%d, want 210/15/10",
			response.Accepted, response.Duplicates, response.Rejected)
	}
}
