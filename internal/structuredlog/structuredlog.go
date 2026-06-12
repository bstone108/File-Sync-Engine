package structuredlog

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"sync"
	"time"
)

type Options struct {
	Level  string
	Output string
}

var (
	writeMu sync.Mutex
	current = sink{minLevel: severityInfo}
)

type sink struct {
	minLevel int
	writer   io.Writer
	closer   io.Closer
}

const (
	severityDebug = iota
	severityInfo
	severityWarn
	severityError
	severityOff
)

// Configure updates the process structured-log sink. Empty options keep the
// default behavior: info-and-above JSON lines written through log.Writer().
func Configure(opts Options) error {
	minLevel, err := parseLevel(opts.Level)
	if err != nil {
		return err
	}
	writer, closer, err := openOutput(opts.Output)
	if err != nil {
		return err
	}
	writeMu.Lock()
	defer writeMu.Unlock()
	if current.closer != nil {
		_ = current.closer.Close()
	}
	current = sink{minLevel: minLevel, writer: writer, closer: closer}
	return nil
}

// Reset restores the default info-level logger-backed sink and closes any
// configured file output. It is primarily useful for tests and embedders.
func Reset() {
	writeMu.Lock()
	defer writeMu.Unlock()
	if current.closer != nil {
		_ = current.closer.Close()
	}
	current = sink{minLevel: severityInfo}
}

// Event writes one structured JSON log record to the configured output.
// It intentionally bypasses log.Printf formatting so daemon log collectors can
// parse each line as JSON while tests and embedders can still redirect the
// standard logger output with log.SetOutput when no explicit output is set.
func Event(level, event, message string, fields map[string]any) {
	levelSeverity, err := parseLevel(level)
	if err != nil {
		levelSeverity = severityInfo
	}
	writeMu.Lock()
	defer writeMu.Unlock()
	if levelSeverity < current.minLevel || current.minLevel == severityOff {
		return
	}
	record := map[string]any{
		"ts":      time.Now().UTC().Format(time.RFC3339Nano),
		"level":   level,
		"event":   event,
		"message": message,
	}
	for key, value := range fields {
		if key == "ts" || key == "level" || key == "event" || key == "message" {
			continue
		}
		record[key] = value
	}
	data, err := json.Marshal(record)
	if err != nil {
		data = []byte(fmt.Sprintf(`{"ts":%q,"level":"error","event":"structured_log_error","message":%q}`, time.Now().UTC().Format(time.RFC3339Nano), err.Error()))
	}
	_, _ = outputWriter().Write(append(data, '\n'))
}

func outputWriter() io.Writer {
	if current.writer != nil {
		return current.writer
	}
	return log.Writer()
}

func parseLevel(level string) (int, error) {
	switch level {
	case "", "info":
		return severityInfo, nil
	case "debug":
		return severityDebug, nil
	case "warn":
		return severityWarn, nil
	case "error":
		return severityError, nil
	case "off":
		return severityOff, nil
	default:
		return 0, fmt.Errorf("invalid structured log level %q", level)
	}
}

func openOutput(output string) (io.Writer, io.Closer, error) {
	switch output {
	case "", "stderr":
		return nil, nil, nil
	case "stdout":
		return os.Stdout, nil, nil
	default:
		file, err := os.OpenFile(output, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
		if err != nil {
			return nil, nil, err
		}
		return file, file, nil
	}
}
