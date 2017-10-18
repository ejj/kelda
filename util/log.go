package util

import (
	"bytes"
	"fmt"
	"strings"
	"time"

	log "github.com/sirupsen/logrus"
)

// Formatter implements the log formatter for Kelda.
type Formatter struct{}

// Format converts a logrus entry into a string for logging.
func (f Formatter) Format(entry *log.Entry) ([]byte, error) {
	b := &bytes.Buffer{}

	level := strings.ToUpper(entry.Level.String())
	fmt.Fprintf(b, "%s [%s] %-40s", level, entry.Time.Format(time.StampMilli),
		entry.Message)

	for k, v := range entry.Data {
		fmt.Fprintf(b, " %s=%+v", k, v)
	}

	b.WriteByte('\n')
	return b.Bytes(), nil
}

// EventTimer is a utility struct that allows us to time how long loops take, as
// well as how often they are triggered.
type EventTimer struct {
	eventName string
	lastStart time.Time
	lastEnd   time.Time
}

// NewEventTimer creates and returns a ready to use EventTimer
func NewEventTimer(eventName string) *EventTimer {
	return &EventTimer{
		eventName: eventName,
		lastEnd:   time.Now(),
		lastStart: time.Time{},
	}
}

// LogStart logs the start of a loop and how long it has been since the last trigger.
func (ltl *EventTimer) LogStart() {
	ltl.lastStart = time.Now()
	log.Debugf("Starting %s event. It has been %v since the last run.",
		ltl.eventName, ltl.lastStart.Sub(ltl.lastEnd))
}

// LogEnd logs the end of a loop and how long it took to run.
func (ltl *EventTimer) LogEnd() {
	ltl.lastEnd = time.Now()
	log.Debugf("%s event ended. It took %v", ltl.eventName,
		ltl.lastEnd.Sub(ltl.lastStart))
}
