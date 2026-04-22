package transfer

import (
	"fmt"
	"sync"
	"time"
)

type Manager struct {
	mu           sync.Mutex
	tasks        map[string]*Task
	statusWriter func(string)
	notify       func(string, string)
}

func NewManager(statusWriter func(string), notify func(string, string)) *Manager {
	return &Manager{
		tasks:        make(map[string]*Task),
		statusWriter: statusWriter,
		notify:       notify,
	}
}

func (m *Manager) Start(id string, direction Direction, fileName string, totalBytes int64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := time.Now()
	m.tasks[id] = &Task{ID: id, Direction: direction, FileName: fileName, TotalBytes: totalBytes, DoneBytes: 0, Status: StatusRunning, StartedAt: now, UpdatedAt: now}
	m.writeStatusLocked(id)
}

func (m *Manager) UpdateProgress(id string, doneBytes, totalBytes int64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	task, ok := m.tasks[id]
	if !ok {
		return
	}
	task.DoneBytes = doneBytes
	if totalBytes > 0 {
		task.TotalBytes = totalBytes
	}
	task.UpdatedAt = time.Now()
	m.writeStatusLocked(id)
}

func (m *Manager) MarkVerifying(id string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if task, ok := m.tasks[id]; ok {
		task.Status = StatusVerifying
		task.UpdatedAt = time.Now()
		m.writeStatusLocked(id)
	}
}

func (m *Manager) MarkCompleted(id string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if task, ok := m.tasks[id]; ok {
		task.Status = StatusCompleted
		task.DoneBytes = task.TotalBytes
		task.UpdatedAt = time.Now()
		if m.statusWriter != nil {
			m.statusWriter("Connected ✓")
		}
		if m.notify != nil {
			m.notify("ClipCascade", fmt.Sprintf("%s 完成: %s", task.Direction, task.FileName))
		}
	}
}

func (m *Manager) MarkFailed(id string, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if task, ok := m.tasks[id]; ok {
		taskErr := "unknown error"
		if err != nil {
			taskErr = err.Error()
		}
		task.Status = StatusFailed
		task.Err = taskErr

		if m.statusWriter != nil {
			m.statusWriter("Transfer Failed")
		}
		if m.notify != nil {
			m.notify("ClipCascade", fmt.Sprintf("%s 失败: %s", task.Direction, task.FileName))
		}
	}
}

func (m *Manager) writeStatusLocked(id string) {
	if m.statusWriter == nil {
		return
	}
	task, ok := m.tasks[id]
	if !ok {
		return
	}
	if task.TotalBytes <= 0 {
		m.statusWriter(fmt.Sprintf("%s %s", task.Direction, task.FileName))
		return
	}
	percent := int(float64(task.DoneBytes) / float64(task.TotalBytes) * 100)
	if percent > 100 {
		percent = 100
	}
	m.statusWriter(fmt.Sprintf("%s %d%%", task.Direction, percent))
}
