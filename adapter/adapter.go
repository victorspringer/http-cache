package adapter

import (
	"sync"

	"github.com/victorspringer/http-cache"
)

// Memory initializes memory adapter.
func Memory() cache.Adapter {
	return &memory{
		sync.Mutex{},
		map[string]cache.Cache{},
	}
}
