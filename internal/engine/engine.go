package engine

import (
	"sync"

	apix "github.com/mnafshin/apix/pkg/api/generated"
)

type Engine struct {
	mu          sync.Mutex
	requests    []*apix.HttpRequest
	subscribers []chan *apix.HttpRequest
}

func New() *Engine {
	return &Engine{}
}

func (e *Engine) AddRequest(req *apix.HttpRequest) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.requests = append(e.requests, req)
	for _, sub := range e.subscribers {
		select {
		case sub <- req:
		default:
		}
	}
}

func (e *Engine) Subscribe() chan *apix.HttpRequest {
	ch := make(chan *apix.HttpRequest, 10)
	e.mu.Lock()
	e.subscribers = append(e.subscribers, ch)
	e.mu.Unlock()
	return ch
}

func (e *Engine) Unsubscribe(ch chan *apix.HttpRequest) {
	e.mu.Lock()
	defer e.mu.Unlock()
	for i, sub := range e.subscribers {
		if sub == ch {
			e.subscribers = append(e.subscribers[:i], e.subscribers[i+1:]...)
			break
		}
	}
	close(ch)
}