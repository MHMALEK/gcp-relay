package history

import (
	"sync"
	"time"

	"github.com/MHMALEK/gcp-relay/internal/cloudevents"
)

const defaultMaxRecords = 200

type Delivery struct {
	Trigger    string `json:"trigger"`
	TargetURL  string `json:"target_url"`
	TargetType string `json:"target_type"`
	Status     string `json:"status"`
	Error      string `json:"error,omitempty"`
}

type Record struct {
	ID         string               `json:"id"`
	Timestamp  time.Time            `json:"timestamp"`
	Source     string               `json:"source"`
	Topic      string               `json:"topic,omitempty"`
	Bucket     string               `json:"bucket"`
	Object     string               `json:"object"`
	EventType  string               `json:"event_type"`
	Event      cloudevents.Envelope `json:"event"`
	Deliveries []Delivery           `json:"deliveries"`
}

type Store struct {
	mu      sync.RWMutex
	records []Record
	max     int
}

func NewStore(max int) *Store {
	if max <= 0 {
		max = defaultMaxRecords
	}
	return &Store{max: max}
}

func (s *Store) Add(rec Record) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.records = append([]Record{rec}, s.records...)
	if len(s.records) > s.max {
		s.records = s.records[:s.max]
	}
}

func (s *Store) List() []Record {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Record, len(s.records))
	copy(out, s.records)
	return out
}

func (s *Store) Get(id string) (Record, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, rec := range s.records {
		if rec.ID == id {
			return rec, true
		}
	}
	return Record{}, false
}
