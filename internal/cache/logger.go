package cache

import (
	"encoding/json"
	"log"
	"time"
)

// Logger is a simple structured logger interface.
type Logger interface {
	Info(msg string, fields map[string]interface{})
	Error(msg string, fields map[string]interface{})
}

// StdLogger implements Logger using the standard log package with JSON output.
type StdLogger struct{}

func (l *StdLogger) Info(msg string, fields map[string]interface{}) {
	fields["level"] = "info"
	fields["msg"] = msg
	fields["ts"] = time.Now().Format(time.RFC3339)
	b, _ := json.Marshal(fields)
	log.Println(string(b))
}

func (l *StdLogger) Error(msg string, fields map[string]interface{}) {
	fields["level"] = "error"
	fields["msg"] = msg
	fields["ts"] = time.Now().Format(time.RFC3339)
	b, _ := json.Marshal(fields)
	log.Println(string(b))
}
