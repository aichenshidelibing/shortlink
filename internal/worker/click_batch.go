package worker

import (
	"context"
	"shortlink/internal/model"
	"shortlink/internal/repository"
	"sync"
	"time"

	"go.uber.org/zap"
)

type ClickWorker struct {
	mu        sync.Mutex
	clickRepo *repository.ClickRepository
	buffer    []*model.Click
	ticker    *time.Ticker
	log       *zap.Logger
}

func NewClickWorker(clickRepo *repository.ClickRepository, log *zap.Logger) *ClickWorker {
	return &ClickWorker{
		clickRepo: clickRepo,
		buffer:    make([]*model.Click, 0, 100),
		ticker:    time.NewTicker(5 * time.Second),
		log:       log,
	}
}

func (w *ClickWorker) Start(ctx context.Context) {
	go func() {
		defer w.ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				w.flush()
				return
			case <-w.ticker.C:
				w.flush()
			}
		}
	}()
}

func (w *ClickWorker) Submit(click *model.Click) {
	w.mu.Lock()
	w.buffer = append(w.buffer, click)
	if len(w.buffer) >= 100 {
		batch := w.buffer
		w.buffer = make([]*model.Click, 0, 100)
		w.mu.Unlock()
		w.flushBatch(batch)
	} else {
		w.mu.Unlock()
	}
}

func (w *ClickWorker) flush() {
	w.mu.Lock()
	if len(w.buffer) == 0 {
		w.mu.Unlock()
		return
	}
	batch := w.buffer
	w.buffer = make([]*model.Click, 0, 100)
	w.mu.Unlock()

	w.flushBatch(batch)
}

func (w *ClickWorker) flushBatch(batch []*model.Click) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := w.clickRepo.CreateBatch(ctx, batch); err != nil {
		w.log.Warn("flush clicks failed", zap.Error(err), zap.Int("count", len(batch)))
	}
}
