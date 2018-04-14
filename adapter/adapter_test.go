package adapter

import (
	"reflect"
	"sync"
	"testing"

	"github.com/victorspringer/http-cache"
)

func TestMemory(t *testing.T) {
	tests := []struct {
		name string
		want cache.Adapter
	}{
		{
			"initializes memory struct",
			&memory{
				sync.Mutex{},
				map[string]cache.Cache{},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Memory(); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("Memory() = %v, want %v", got, tt.want)
			}
		})
	}
}
