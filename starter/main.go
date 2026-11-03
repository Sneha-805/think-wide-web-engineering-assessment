package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"sync"
	"time"
)

const maxRequestBodyBytes = 1 << 20

type CampaignStats struct {
	CampaignID  string `json:"campaign_id"`
	Sent        int    `json:"sent"`
	Delivered   int    `json:"delivered"`
	Opened      int    `json:"opened"`
	Clicked     int    `json:"clicked"`
	UniqueOpens int    `json:"unique_opens"`
}

type Store struct {
	mu       sync.RWMutex
	stats    map[string]*CampaignStats
	seen     map[string]Event
	openedBy map[string]map[string]bool
}

var store = &Store{
	stats:    make(map[string]*CampaignStats),
	seen:     make(map[string]Event),
	openedBy: make(map[string]map[string]bool),
}

func main() {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /events", handlePostEvents)
	mux.HandleFunc("GET /campaigns/{campaignID}/stats", handleGetStats)

	addr := ":8080"
	fmt.Printf("listening on %s\n", addr)
	server := &http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	log.Fatal(server.ListenAndServe())
}

// handlePostEvents ingests a JSON array of events.
func handlePostEvents(w http.ResponseWriter, r *http.Request) {
	if r.ContentLength > maxRequestBodyBytes {
		writeJSON(w, http.StatusRequestEntityTooLarge, map[string]any{"error": "request body exceeds 1 MiB limit"})
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBodyBytes)

	var raw []json.RawMessage
	decoder := json.NewDecoder(r.Body)
	if err := decoder.Decode(&raw); err != nil {
		var maxBytesErr *http.MaxBytesError
		if errors.As(err, &maxBytesErr) {
			writeJSON(w, http.StatusRequestEntityTooLarge, map[string]any{"error": "request body exceeds 1 MiB limit"})
			return
		}
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "request body must be a JSON array"})
		return
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "request body contains trailing JSON values"})
			return
		}
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid trailing JSON"})
		return
	}
	if raw == nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "request body must be a JSON array"})
		return
	}

	results := make([]map[string]any, 0, len(raw))
	accepted, duplicates, rejected := 0, 0, 0
	for i, item := range raw {
		var event Event
		decoder := json.NewDecoder(bytes.NewReader(item))
		err := decoder.Decode(&event)
		if err == nil {
			err = validateEvent(&event)
		}
		if err != nil {
			rejected++
			results = append(results, map[string]any{"index": i, "status": "rejected", "error": err.Error()})
			continue
		}

		status, conflict := store.accept(event)
		switch status {
		case "accepted":
			accepted++
			results = append(results, map[string]any{"index": i, "status": "accepted", "event_id": event.EventID})
		case "duplicate":
			duplicates++
			results = append(results, map[string]any{"index": i, "status": "duplicate", "event_id": event.EventID})
		case "rejected":
			rejected++
			results = append(results, map[string]any{"index": i, "status": "rejected", "event_id": event.EventID, "error": conflict})
		}
	}

	status := http.StatusOK
	if rejected > 0 && accepted == 0 && duplicates == 0 {
		status = http.StatusBadRequest
	} else if rejected > 0 {
		status = http.StatusMultiStatus
	}
	writeJSON(w, status, map[string]any{
		"accepted":   accepted,
		"duplicates": duplicates,
		"rejected":   rejected,
		"results":    results,
	})
}

// handleGetStats returns aggregated stats for one campaign.
func handleGetStats(w http.ResponseWriter, r *http.Request) {
	campaignID := r.PathValue("campaignID")
	stats, ok := store.statsFor(campaignID)
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "campaign not found"})
		return
	}
	writeJSON(w, http.StatusOK, stats)
}

func validateEvent(event *Event) error {
	if event.EventID == "" {
		return fmt.Errorf("event_id is required")
	}
	if event.CampaignID == "" {
		return fmt.Errorf("campaign_id is required")
	}
	if event.ContactID == "" {
		return fmt.Errorf("contact_id is required")
	}
	switch event.Type {
	case "sent", "delivered", "opened", "clicked":
	default:
		return fmt.Errorf("type must be sent, delivered, opened, or clicked")
	}
	timestamp, err := time.Parse(time.RFC3339, event.Timestamp)
	if err != nil {
		return fmt.Errorf("timestamp must be RFC3339")
	}
	event.Timestamp = timestamp.UTC().Format(time.RFC3339Nano)
	return nil
}

func (s *Store) accept(event Event) (status string, conflict string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if existing, ok := s.seen[event.EventID]; ok {
		if sameEvent(existing, event) {
			return "duplicate", ""
		}
		return "rejected", "event_id conflicts with a previously received event"
	}

	s.seen[event.EventID] = event
	stats, ok := s.stats[event.CampaignID]
	if !ok {
		stats = &CampaignStats{CampaignID: event.CampaignID}
		s.stats[event.CampaignID] = stats
	}
	switch event.Type {
	case "sent":
		stats.Sent++
	case "delivered":
		stats.Delivered++
	case "opened":
		stats.Opened++
		if s.openedBy[event.CampaignID] == nil {
			s.openedBy[event.CampaignID] = make(map[string]bool)
		}
		if !s.openedBy[event.CampaignID][event.ContactID] {
			s.openedBy[event.CampaignID][event.ContactID] = true
			stats.UniqueOpens++
		}
	case "clicked":
		stats.Clicked++
	}
	return "accepted", ""
}

func (s *Store) statsFor(campaignID string) (CampaignStats, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	stats, ok := s.stats[campaignID]
	if !ok {
		return CampaignStats{}, false
	}
	return *stats, true
}

func sameEvent(a, b Event) bool {
	ab, errA := json.Marshal(a)
	bb, errB := json.Marshal(b)
	return errA == nil && errB == nil && string(ab) == string(bb)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		log.Printf("writeJSON: %v", err)
	}
}
