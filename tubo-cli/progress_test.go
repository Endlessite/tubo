package main

import (
	"encoding/json"
	"testing"
)

func TestNewStreamMessageSize(t *testing.T) {
	cases := []struct {
		name           string
		isDir, isStdin bool
		size, want     int64
	}{
		{"regular file", false, false, 3000, 3000},
		{"stdin", false, true, 0, 0},
		{"directory", true, false, 5000, 0},
	}
	for _, c := range cases {
		msg := newStreamMessage(true, c.isDir, c.isStdin, c.size)
		if msg.Type != "stream" || !msg.Compressed || msg.Directory != c.isDir {
			t.Errorf("%s: unexpected message %+v", c.name, msg)
		}
		if msg.Size != c.want {
			t.Errorf("%s: Size = %d, want %d", c.name, msg.Size, c.want)
		}
	}
}

func TestStreamMessageSizeJSON(t *testing.T) {
	b, _ := json.Marshal(StreamMessage{Type: "stream", Size: 42})
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	if m["size"] != float64(42) {
		t.Errorf("size field = %v, want 42 (json: %s)", m["size"], b)
	}

	// Older senders don't send a size; it must decode as 0 (unknown).
	var old StreamMessage
	if err := json.Unmarshal([]byte(`{"type":"stream","compressed":false,"directory":false}`), &old); err != nil {
		t.Fatal(err)
	}
	if old.Size != 0 {
		t.Errorf("Size from old sender = %d, want 0", old.Size)
	}
}

func TestFormatProgress(t *testing.T) {
	cases := []struct {
		done, total int64
		want        string
	}{
		{1536, 0, "Receiving: 1.5 KB"},
		{0, 2048, "Receiving: 0.0% | 0 B / 2.0 KB"},
		{1024, 2048, "Receiving: 50.0% | 1.0 KB / 2.0 KB"},
		{4096, 2048, "Receiving: 100.0% | 4.0 KB / 2.0 KB"},
	}
	for _, c := range cases {
		if got := formatProgress("Receiving", c.done, c.total); got != c.want {
			t.Errorf("formatProgress(%d, %d) = %q, want %q", c.done, c.total, got, c.want)
		}
	}
}
