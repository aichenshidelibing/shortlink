package snowflake

import (
	"sync"
	"time"
)

const (
	epoch           = int64(1704067200000) // 2024-01-01 00:00:00 UTC
	workerIDBits    = uint(5)
	datacenterBits  = uint(5)
	sequenceBits    = uint(12)
	workerIDShift   = sequenceBits
	datacenterShift = sequenceBits + workerIDBits
	timestampShift  = sequenceBits + workerIDBits + datacenterBits
	sequenceMask    = int64(-1) ^ (int64(-1) << sequenceBits)
	maxWorkerID     = int64(-1) ^ (int64(-1) << workerIDBits)
	maxDatacenterID = int64(-1) ^ (int64(-1) << datacenterBits)
)

type Generator struct {
	mu           sync.Mutex
	lastTimestamp int64
	workerID     int64
	datacenterID int64
	sequence     int64
}

func NewGenerator(workerID, datacenterID int64) *Generator {
	if workerID < 0 || workerID > maxWorkerID {
		workerID = 0
	}
	if datacenterID < 0 || datacenterID > maxDatacenterID {
		datacenterID = 0
	}
	return &Generator{
		workerID:     workerID,
		datacenterID: datacenterID,
	}
}

func (g *Generator) NextID() int64 {
	g.mu.Lock()
	defer g.mu.Unlock()

	now := time.Now().UnixMilli()
	if now < g.lastTimestamp {
		now = g.lastTimestamp
	}

	if now == g.lastTimestamp {
		g.sequence = (g.sequence + 1) & sequenceMask
		if g.sequence == 0 {
			for now <= g.lastTimestamp {
				now = time.Now().UnixMilli()
			}
		}
	} else {
		g.sequence = 0
	}

	g.lastTimestamp = now
	id := ((now - epoch) << timestampShift) |
		(g.datacenterID << datacenterShift) |
		(g.workerID << workerIDShift) |
		g.sequence
	return id
}
