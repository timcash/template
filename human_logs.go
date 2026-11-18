package main

import (
	"fmt"
	"io"
	"path"
	"runtime"
	"strconv"
	"strings"
	"time"
	"unicode"
)

// humanRecord's source is the reporting callsite. The Go test event carries
// output as arbitrary text: preserve its original source hints in the message,
// never promote an untrusted child prefix into the record metadata.
func humanRecord(at time.Time, level, message string) string {
	if at.IsZero() {
		at = time.Now()
	}
	_, file, line, _ := runtime.Caller(1)
	source := fmt.Sprintf("%s:%d", sourceBase(file), line)
	// Keep one record per line and neutralize child terminal control characters.
	var safe strings.Builder
	for _, r := range message {
		if unicode.IsControl(r) {
			quoted := strconv.QuoteRune(r)
			safe.WriteString(quoted[1 : len(quoted)-1])
		} else {
			safe.WriteRune(r)
		}
	}
	return fmt.Sprintf("%s %s %s %s\n", at.UTC().Format(time.RFC3339Nano), level, source, safe.String())
}

func sourceBase(file string) string {
	return path.Base(strings.ReplaceAll(file, `\`, "/"))
}

func writeRecord(out io.Writer, run *activeTest, line string) error {
	if _, err := io.WriteString(out, line); err != nil {
		return err
	}
	if run != nil {
		_, err := io.WriteString(run.log, line)
		return err
	}
	return nil
}
