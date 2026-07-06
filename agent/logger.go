package main

import (
	"encoding/json"
	"log"
	"os"
	"sync"
	"time"
)

// logger écrit un log local append-only de tout ce qui se passe côté
// agent : connexions, requêtes reçues, confirmations, exécutions, erreurs.
type Logger struct {
	mu   sync.Mutex
	file *os.File
}

func NewLogger(path string) (*Logger, error) {
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return nil, err
	}
	return &Logger{file: f}, nil
}

type logEntry struct {
	Time    string      `json:"time"`
	Event   string      `json:"event"`
	Command string      `json:"command,omitempty"`
	Details interface{} `json:"details,omitempty"`
}

// Log ajoute une ligne JSON au fichier de log. Ne bloque jamais l'exécution
// principale : une erreur d'écriture est seulement journalisée sur stderr.
func (l *Logger) Log(event, command string, details interface{}) {
	l.mu.Lock()
	defer l.mu.Unlock()

	entry := logEntry{
		Time:    time.Now().Format(time.RFC3339),
		Event:   event,
		Command: command,
		Details: details,
	}
	b, err := json.Marshal(entry)
	if err != nil {
		log.Printf("logger: erreur d'encodage: %v", err)
		return
	}
	if _, err := l.file.Write(append(b, '\n')); err != nil {
		log.Printf("logger: erreur d'écriture: %v", err)
	}
}

func (l *Logger) Close() error {
	return l.file.Close()
}
