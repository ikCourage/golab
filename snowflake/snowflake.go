package snowflake

import (
	"fmt"
	"sync"
	"time"
)

type snowFlake struct {
	lk            sync.Mutex
	epoch         int64
	lastTimestamp int64
	workerId      int64
	sequence      int64
}

type SnowFlake interface {
	NextId() (int64, error)
}

type Config struct {
	Epoch    int64
	WorkerId int64
}

var (
	defaultConfig = &Config{
		Epoch: 1578499200000,
	}
)

func New(config *Config) SnowFlake {
	if nil == config {
		config = defaultConfig
	}
	return &snowFlake{
		epoch:    config.Epoch,
		workerId: config.WorkerId,
	}
}

func (self *snowFlake) NextId() (int64, error) {
	self.lk.Lock()
	timestamp := time.Now().UnixNano() / 1e6
	if timestamp < self.lastTimestamp {
		self.lk.Unlock()
		return 0, fmt.Errorf("clock is back: %d from previous: %d", timestamp, self.lastTimestamp)
		timestamp = self.lastTimestamp
	}
	if timestamp == self.lastTimestamp {
		self.sequence = (self.sequence + 1) & 4095
		if self.sequence == 0 {
			timestamp++
			time.Sleep(time.Duration(timestamp*1e6 - time.Now().UnixNano()))
		}
	} else {
		self.sequence = 0
	}
	self.lastTimestamp = timestamp
	self.lk.Unlock()
	id := ((timestamp - self.epoch) << 22) |
		(self.workerId << 12) |
		self.sequence
	return id, nil
}
