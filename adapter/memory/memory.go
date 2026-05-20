/*
MIT License

Copyright (c) 2018 Victor Springer

Permission is hereby granted, free of charge, to any person obtaining a copy
of this software and associated documentation files (the "Software"), to deal
in the Software without restriction, including without limitation the rights
to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
copies of the Software, and to permit persons to whom the Software is
furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in all
copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
SOFTWARE.
*/

package memory

import (
	"errors"
	"fmt"
	"math"
	"sync"
	"sync/atomic"
	"time"

	cache "github.com/victorspringer/http-cache"
)

// Algorithm is the string type for caching algorithms labels.
type Algorithm string

const (
	// LRU is the constant for Least Recently Used.
	LRU Algorithm = "LRU"

	// MRU is the constant for Most Recently Used.
	MRU Algorithm = "MRU"

	// LFU is the constant for Least Frequently Used.
	LFU Algorithm = "LFU"

	// MFU is the constant for Most Frequently Used.
	MFU Algorithm = "MFU"
)

// entry holds per-key access metadata. lastAccessNano and frequency are
// read and updated atomically so cache hits can record their access
// outside of the adapter's write lock.
type entry struct {
	lastAccessNano int64
	frequency      int64
	size           int
}

// Adapter is the memory adapter data structure.
type Adapter struct {
	mutex     sync.RWMutex
	capacity  int
	algorithm Algorithm
	store     map[uint64][]byte
	meta      map[uint64]*entry
	storage   storageControl
}

// AdapterOptions is used to set Adapter settings.
type AdapterOptions func(a *Adapter) error

// Get implements the cache Adapter interface Get method.
func (a *Adapter) Get(key uint64) ([]byte, bool) {
	a.mutex.RLock()
	response, ok := a.store[key]
	a.mutex.RUnlock()

	if ok {
		return response, true
	}

	return nil, false
}

// Set implements the cache Adapter interface Set method.
func (a *Adapter) Set(key uint64, response []byte, expiration time.Time) {
	a.mutex.Lock()
	defer a.mutex.Unlock()

	if a.meta == nil {
		// Backstop for adapters constructed without NewAdapter (e.g.
		// direct struct literals in tests).
		a.meta = make(map[uint64]*entry, len(a.store)+1)
	}

	if old, ok := a.store[key]; ok {
		delete(a.store, key)
		delete(a.meta, key)
		a.storage.del(len(old))
	}

	if !a.storage.canCache(len(response)) {
		return
	}

	// New key, make sure we have the capacity.
	if a.capacity > 0 && len(a.store) == a.capacity {
		a.evict()
	}

	// now evict based on storage
	for a.storage.shouldEvict(len(response)) {
		if !a.evict() {
			return
		}
	}

	a.store[key] = response
	a.meta[key] = newEntryFromResponse(response, len(response))
	a.storage.add(len(response))
}

// Touch implements the cache.AdapterTouch optional interface. It records
// an access on key without touching the cached payload or the write
// lock, eliminating the read-modify-write race that the legacy
// middleware path exhibited under concurrency.
func (a *Adapter) Touch(key uint64) {
	a.mutex.RLock()
	e, ok := a.meta[key]
	a.mutex.RUnlock()
	if !ok {
		return
	}
	atomic.StoreInt64(&e.lastAccessNano, time.Now().UnixNano())
	atomic.AddInt64(&e.frequency, 1)
}

// Release implements the Adapter interface Release method.
func (a *Adapter) Release(key uint64) {
	a.mutex.Lock()
	defer a.mutex.Unlock()

	b, ok := a.store[key]
	if !ok {
		return
	}
	delete(a.store, key)
	delete(a.meta, key)
	a.storage.del(len(b))
}

// newEntryFromResponse seeds an entry from the encoded Response's
// metadata when possible. This keeps eviction semantics compatible with
// callers that pre-populated the store with hand-built Response blobs.
func newEntryFromResponse(blob []byte, size int) *entry {
	e := &entry{
		lastAccessNano: time.Now().UnixNano(),
		frequency:      1,
		size:           size,
	}
	if r := cache.BytesToResponse(blob); !r.LastAccess.IsZero() || r.Frequency > 0 {
		if !r.LastAccess.IsZero() {
			e.lastAccessNano = r.LastAccess.UnixNano()
		}
		if r.Frequency > 0 {
			e.frequency = int64(r.Frequency)
		}
	}
	return e
}

// evict removes a single entry from the store. It assumes that the caller holds
// the write lock.
func (a *Adapter) evict() bool {
	var (
		selectedKey uint64
		selSize     int
		hit         bool
	)

	switch a.algorithm {
	case LRU:
		oldest := int64(math.MaxInt64)
		for k, e := range a.meta {
			last := atomic.LoadInt64(&e.lastAccessNano)
			if last < oldest {
				oldest = last
				selectedKey = k
				selSize = e.size
				hit = true
			}
		}
	case MRU:
		newest := int64(math.MinInt64)
		for k, e := range a.meta {
			last := atomic.LoadInt64(&e.lastAccessNano)
			if last >= newest {
				newest = last
				selectedKey = k
				selSize = e.size
				hit = true
			}
		}
	case LFU:
		lowest := int64(math.MaxInt64)
		for k, e := range a.meta {
			f := atomic.LoadInt64(&e.frequency)
			if f < lowest {
				lowest = f
				selectedKey = k
				selSize = e.size
				hit = true
			}
		}
	case MFU:
		highest := int64(math.MinInt64)
		for k, e := range a.meta {
			f := atomic.LoadInt64(&e.frequency)
			if f >= highest {
				highest = f
				selectedKey = k
				selSize = e.size
				hit = true
			}
		}
	}

	if hit {
		a.storage.del(selSize)
		delete(a.store, selectedKey)
		delete(a.meta, selectedKey)
	}
	return hit
}

// NewAdapter initializes memory adapter.
func NewAdapter(opts ...AdapterOptions) (cache.Adapter, error) {
	a := &Adapter{}

	for _, opt := range opts {
		if err := opt(a); err != nil {
			return nil, err
		}
	}

	if a.capacity <= 1 && a.storage.active() == false {
		return nil, errors.New("memory adapter capacity is not set")
	}

	if a.algorithm == "" {
		return nil, errors.New("memory adapter caching algorithm is not set")
	}

	a.mutex = sync.RWMutex{}
	if a.capacity > 0 {
		a.store = make(map[uint64][]byte, a.capacity)
		a.meta = make(map[uint64]*entry, a.capacity)
	} else {
		a.store = make(map[uint64][]byte, 4) //just init with something
		a.meta = make(map[uint64]*entry, 4)
	}

	return a, nil
}

// AdapterWithAlgorithm sets the approach used to select a cached
// response to be evicted when the capacity is reached.
func AdapterWithAlgorithm(alg Algorithm) AdapterOptions {
	return func(a *Adapter) error {
		a.algorithm = alg
		return nil
	}
}

// AdapterWithCapacity sets the maximum number of cached responses.
func AdapterWithCapacity(cap int) AdapterOptions {
	return func(a *Adapter) error {
		if cap <= 1 {
			return fmt.Errorf("memory adapter requires a capacity greater than %v", cap)
		}

		a.capacity = cap

		return nil
	}
}

// AdapterWithStorageCapacity sets the maximum number of cached bytes
func AdapterWithStorageCapacity(cap int) AdapterOptions {
	return func(a *Adapter) error {
		if cap <= 0 {
			return errors.New("memory adapter requires a storage capacity greater than 0")
		}

		a.storage = storageControl{
			max: cap,
		}

		return nil
	}
}

type storageControl struct {
	max int
	cur int
}

func (s *storageControl) active() bool {
	return s.max > 0
}

func (s *storageControl) add(v int) {
	if v >= 0 {
		s.cur += v // if you roll over an int64, well... sorry?
	}
}

func (s *storageControl) del(v int) {
	if v >= 0 {
		if s.cur = s.cur - v; s.cur < 0 {
			s.cur = 0 // safety check it
		}
	}
}

// storageShouldEvict will return true if the proposed new bytes plus current exceeds our max
// we will NOT evict our max is set to 0 (e.g. we are not tracking total bytes)
func (s *storageControl) shouldEvict(newBytes int) bool {
	if s.max <= 0 {
		return false // basically "we have no opinion"
	}
	if next := (s.cur + newBytes); next < 0 || next > s.max {
		return true
	}
	return false
}

func (s *storageControl) canCache(newBytes int) bool {
	if s.max <= 0 {
		return true // we have no opinion
	}
	return s.max >= newBytes
}
