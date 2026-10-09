package blobstore

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"sort"
	"strings"
	"sync"
	"time"
)

// Memory — хранилище в памяти для тестов и для запуска без S3. Ведёт себя как настоящее: объект появляется после
// успешной записи целиком, повторное удаление не ошибка, Walk идёт по порядку ключей.
type Memory struct {
	mu   sync.Mutex
	objs map[string]memObject
	// Now — часы для времени записи; по умолчанию time.Now.
	Now func() time.Time
	// Fault, если задана, вызывается перед каждой операцией ("put", "get", "delete", "walk", "ping") и может вернуть ошибку.
	Fault func(op, key string) error
}

type memObject struct {
	data []byte
	mod  time.Time
}

// NewMemory создаёт пустое хранилище.
func NewMemory() *Memory { return &Memory{objs: map[string]memObject{}} }

func (m *Memory) now() time.Time {
	if m.Now != nil {
		return m.Now()
	}
	return time.Now()
}

func (m *Memory) fault(op, key string) error {
	if m.Fault == nil {
		return nil
	}
	return m.Fault(op, key)
}

// Put записывает объект.
func (m *Memory) Put(_ context.Context, key string, r io.Reader) (int64, error) {
	if err := m.fault("put", key); err != nil {
		return 0, err
	}
	data, err := io.ReadAll(r)
	if err != nil {
		return 0, fmt.Errorf("blobstore: read %s: %w", key, err)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.objs[key] = memObject{data: data, mod: m.now()}
	return int64(len(data)), nil
}

type memReader struct {
	*bytes.Reader
}

func (memReader) Close() error { return nil }

func (r memReader) Size() int64 { return r.Reader.Size() }

// Get открывает объект.
func (m *Memory) Get(_ context.Context, key string) (Object, error) {
	if err := m.fault("get", key); err != nil {
		return nil, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	o, ok := m.objs[key]
	if !ok {
		return nil, ErrNotFound
	}
	return memReader{bytes.NewReader(o.data)}, nil
}

// Delete удаляет объекты.
func (m *Memory) Delete(_ context.Context, keys ...string) error {
	for _, k := range keys {
		if err := m.fault("delete", k); err != nil {
			return err
		}
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, k := range keys {
		delete(m.objs, k)
	}
	return nil
}

// Walk перебирает объекты по порядку ключей.
func (m *Memory) Walk(_ context.Context, prefix string, fn func(Info) error) error {
	if err := m.fault("walk", prefix); err != nil {
		return err
	}
	m.mu.Lock()
	infos := make([]Info, 0, len(m.objs))
	for k, o := range m.objs {
		if strings.HasPrefix(k, prefix) {
			infos = append(infos, Info{Key: k, Size: int64(len(o.data)), ModTime: o.mod})
		}
	}
	m.mu.Unlock()
	sort.Slice(infos, func(i, j int) bool { return infos[i].Key < infos[j].Key })
	for _, in := range infos {
		if err := fn(in); err != nil {
			return err
		}
	}
	return nil
}

// Ping всегда отвечает, если не задан сбой.
func (m *Memory) Ping(_ context.Context) error { return m.fault("ping", "") }

// Count — сколько объектов лежит (для тестов).
func (m *Memory) Count() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.objs)
}

// Has — есть ли объект (для тестов).
func (m *Memory) Has(key string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	_, ok := m.objs[key]
	return ok
}

// Keys — все ключи по порядку (для тестов).
func (m *Memory) Keys() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]string, 0, len(m.objs))
	for k := range m.objs {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// Corrupt портит содержимое объекта (для тестов проверки контрольной суммы).
func (m *Memory) Corrupt(key string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if o, ok := m.objs[key]; ok {
		o.data = append([]byte("x"), o.data...)
		m.objs[key] = o
	}
}
