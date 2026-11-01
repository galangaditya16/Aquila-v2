package main

import (
	"sync"
	"time"
)

// maxLogEntries adalah batas entri log yang disimpan. Buffer berbentuk ring
// dengan memori tetap agar aplikasi tidak membengkak saat berjalan lama.
const maxLogEntries = 1000

// maxLogRows adalah batas baris log yang dikirim ke UI sekaligus.
const maxLogRows = 200

type LogEntry struct {
	Time    string `json:"time"`
	Level   string `json:"level"`
	Message string `json:"message"`
}

// LogBuffer adalah penyimpanan log berbentuk ring buffer berkapasitas tetap.
type LogBuffer struct {
	mu      sync.RWMutex
	entries []LogEntry
	next    int
	count   int
}

func NewLogBuffer() *LogBuffer {
	return &LogBuffer{entries: make([]LogEntry, maxLogEntries)}
}

// Add menyimpan satu entri dan mengembalikan salinannya.
func (b *LogBuffer) Add(level, message string) LogEntry {
	entry := LogEntry{
		Time:    time.Now().Format("2006-01-02 15:04:05"),
		Level:   level,
		Message: message,
	}
	b.mu.Lock()
	b.entries[b.next] = entry
	b.next = (b.next + 1) % maxLogEntries
	if b.count < maxLogEntries {
		b.count++
	}
	b.mu.Unlock()
	return entry
}

// Latest mengembalikan hingga limit entri terbaru, urut lama ke baru.
func (b *LogBuffer) Latest(limit int) []LogEntry {
	if limit <= 0 || limit > maxLogRows {
		limit = maxLogRows
	}
	b.mu.RLock()
	defer b.mu.RUnlock()
	if b.count == 0 {
		return []LogEntry{}
	}
	size := b.count
	if size > limit {
		size = limit
	}
	out := make([]LogEntry, 0, size)
	start := (b.next - size + maxLogEntries) % maxLogEntries
	for i := 0; i < size; i++ {
		out = append(out, b.entries[(start+i)%maxLogEntries])
	}
	return out
}

// Clear mengosongkan buffer.
func (b *LogBuffer) Clear() {
	b.mu.Lock()
	b.next = 0
	b.count = 0
	b.mu.Unlock()
}
