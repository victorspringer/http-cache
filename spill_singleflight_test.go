package cache

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"
)

type spillTestWriter struct {
	*httptest.ResponseRecorder
	headerWrites int
	writeCalls   int
	failAt       int
	failN        int
	writeErr     error
}

func (w *spillTestWriter) WriteHeader(status int) {
	w.headerWrites++
	w.ResponseRecorder.WriteHeader(status)
}

func (w *spillTestWriter) Write(b []byte) (int, error) {
	w.writeCalls++
	if w.writeCalls == w.failAt {
		if w.failN > 0 {
			if n, err := w.ResponseRecorder.Write(b[:w.failN]); err != nil {
				return n, err
			}
		}
		return w.failN, w.writeErr
	}
	return w.ResponseRecorder.Write(b)
}

func TestClientWithSingleflightSpillsLeaderResponse(t *testing.T) {
	for _, tt := range []struct {
		name   string
		status int
		chunks []string
		stored bool
	}{
		{name: "single write", status: http.StatusCreated, chunks: []string{"abcdefghij"}},
		{name: "multiple writes", status: http.StatusCreated, chunks: []string{"abc", "de", "fghi", "jk"}},
		{name: "implicit status", chunks: []string{"abcdefghij", "kl"}},
		{name: "below limit", status: http.StatusCreated, chunks: []string{"ab", "cd"}, stored: true},
		{name: "at limit", status: http.StatusCreated, chunks: []string{"abcd", "efgh"}, stored: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			const url = "http://x/leader-spill"
			adapter := &slowAdapter{store: map[uint64][]byte{}}
			client, err := NewClient(
				ClientWithAdapter(adapter),
				ClientWithTTL(time.Minute),
				ClientWithSingleflight(),
				ClientWithMaxBodySize(8),
			)
			if err != nil {
				t.Fatal(err)
			}
			want := strings.Join(tt.chunks, "")
			cookies := []string{"a=1; Path=/", "b=2; Path=/"}
			values := []string{"one", "two"}
			var output *spillTestWriter
			calls := 0
			handler := client.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				w.Header()["Set-Cookie"] = cookies
				w.Header()["X-Values"] = values
				w.Header().Set(cacheStatusCodeHeader, "499")
				if tt.status != 0 {
					w.WriteHeader(tt.status)
				}
				for _, chunk := range tt.chunks {
					if n, err := w.Write([]byte(chunk)); n != len(chunk) || err != nil {
						t.Fatalf("Write = %d, %v, want %d, nil", n, err, len(chunk))
					}
				}
				if tt.stored {
					if output.Body.Len() != 0 || output.headerWrites != 0 {
						t.Error("below-limit response was forwarded before capture completed")
					}
				} else if output.Body.String() != want {
					t.Errorf("body before handler returned = %q, want %q", output.Body.String(), want)
				}
			}))
			status := tt.status
			if status == 0 {
				status = http.StatusOK
			}
			for i := 0; i < 2; i++ {
				output = &spillTestWriter{ResponseRecorder: httptest.NewRecorder()}
				handler.ServeHTTP(output, httptest.NewRequest(http.MethodGet, url, nil))
				if output.Code != status || output.Body.String() != want {
					t.Errorf("response %d = %d %q, want %d %q", i, output.Code, output.Body.String(), status, want)
				}
				if output.headerWrites != 1 {
					t.Errorf("response %d status writes = %d, want 1", i, output.headerWrites)
				}
				headers := output.Result().Header
				if !reflect.DeepEqual(headers.Values("Set-Cookie"), cookies) || !reflect.DeepEqual(headers.Values("X-Values"), values) {
					t.Errorf("response %d headers = %v", i, headers)
				}
				if headers.Get(cacheStatusCodeHeader) != "" {
					t.Errorf("response %d leaked internal status header", i)
				}
			}
			stored, ok := adapter.Get(generateKey(url))
			if ok != tt.stored {
				t.Errorf("response stored = %v, want %v", ok, tt.stored)
			}
			if tt.stored && string(BytesToResponse(stored).Value) != want {
				t.Errorf("cached body = %q, want %q", BytesToResponse(stored).Value, want)
			}
			wantCalls := 2
			if tt.stored {
				wantCalls = 1
			}
			if calls != wantCalls {
				t.Errorf("origin calls = %d, want %d", calls, wantCalls)
			}
		})
	}
}

func TestCaptureWriterSpillWriteErrors(t *testing.T) {
	writeErr := errors.New("spill write failed")
	for _, tt := range []struct {
		name     string
		failAt   int
		failN    int
		writeErr error
		wantN    int
		wantErr  error
		tail     bool
	}{
		{name: "prefix error", failAt: 1, writeErr: writeErr, wantErr: writeErr},
		{name: "short prefix", failAt: 1, failN: 1, wantErr: io.ErrShortWrite},
		{name: "crossing chunk", failAt: 2, failN: 1, writeErr: writeErr, wantN: 1, wantErr: writeErr},
		{name: "after spill", failAt: 3, failN: 1, writeErr: writeErr, wantN: 1, wantErr: writeErr, tail: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			output := &spillTestWriter{
				ResponseRecorder: httptest.NewRecorder(),
				failAt:           tt.failAt,
				failN:            tt.failN,
				writeErr:         tt.writeErr,
			}
			cw := newCaptureWriter(4)
			cw.spill = output
			if n, err := cw.Write([]byte("ab")); n != 2 || err != nil {
				t.Fatalf("buffered Write = %d, %v, want 2, nil", n, err)
			}
			n, err := cw.Write([]byte("cdef"))
			if tt.tail {
				if n != 4 || err != nil {
					t.Fatalf("spill Write = %d, %v, want 4, nil", n, err)
				}
				n, err = cw.Write([]byte("gh"))
			}
			if n != tt.wantN || !errors.Is(err, tt.wantErr) {
				t.Errorf("Write = %d, %v, want %d, %v", n, err, tt.wantN, tt.wantErr)
			}
			if output.writeCalls != tt.failAt || output.headerWrites != 1 {
				t.Errorf("destination writes = %d, status writes = %d", output.writeCalls, output.headerWrites)
			}
		})
	}
}
