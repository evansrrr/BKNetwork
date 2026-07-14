package events

import (
	"sync"
	"time"
)

type Event struct {
	Type    string      `json:"type"`
	Message string      `json:"message"`
	Data    interface{} `json:"data,omitempty"`
	Time    time.Time   `json:"time"`
}

type Hub struct {
	mu          sync.RWMutex
	subscribers map[chan Event]struct{}
	last        Event
	hasLast     bool
	ticker      *time.Ticker
	tickerDone  chan struct{}
	onTick      func() Event
}

func NewHub() *Hub {
	return &Hub{subscribers: make(map[chan Event]struct{})}
}

func (h *Hub) Subscribe(buffer int) chan Event {
	if buffer <= 0 {
		buffer = 8
	}
	ch := make(chan Event, buffer)
	h.mu.Lock()
	h.subscribers[ch] = struct{}{}
	last := h.last
	hasLast := h.hasLast
	count := len(h.subscribers)
	h.mu.Unlock()

	if count == 1 {
		h.StartTicker()
	}

	if hasLast {
		select {
		case ch <- last:
		default:
		}
	}
	return ch
}

func (h *Hub) Unsubscribe(ch chan Event) {
	h.mu.Lock()
	if _, ok := h.subscribers[ch]; ok {
		delete(h.subscribers, ch)
		close(ch)
	}
	count := len(h.subscribers)
	h.mu.Unlock()

	if count == 0 {
		h.StopTicker()
	}
}

func (h *Hub) StartTicker() {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.ticker != nil {
		return
	}
	h.ticker = time.NewTicker(3 * time.Second)
	h.tickerDone = make(chan struct{})
	go func() {
		for {
			select {
			case <-h.ticker.C:
				h.mu.RLock()
				fn := h.onTick
				h.mu.RUnlock()
				if fn != nil {
					if event := fn(); event.Type != "" {
						h.Publish(event)
					}
				}
			case <-h.tickerDone:
				return
			}
		}
	}()
}

func (h *Hub) StopTicker() {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.ticker == nil {
		return
	}
	h.ticker.Stop()
	close(h.tickerDone)
	h.ticker = nil
	h.tickerDone = nil
}

func (h *Hub) SetOnTick(fn func() Event) {
	h.mu.Lock()
	h.onTick = fn
	h.mu.Unlock()
}

func (h *Hub) Publish(event Event) {
	event.Time = time.Now().UTC()
	h.mu.Lock()
	h.last = event
	h.hasLast = true
	for ch := range h.subscribers {
		select {
		case ch <- event:
		default:
		}
	}
	h.mu.Unlock()
}

func (h *Hub) Snapshot() (Event, bool) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.last, h.hasLast
}
