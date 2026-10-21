package log_test

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/dfryer1193/mjolnir/utils/log"
)

// These tests exercise logger behaviour through GetLoggerTo, which writes only
// to a per-test writer and never touches the process-wide sinks. They share no
// mutable state, so every one runs in parallel. The stateful machinery (sink
// swapping, level changes, SetJSONFile) is covered by the internal tests
// against isolated cores.

// Every line carries the component name as the logger field.
func TestGetLoggerNamesTheStream(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	lg := log.GetLoggerTo("OrderService", &buf)
	lg.Info("processing order")

	out := buf.String()
	if !strings.Contains(out, `"logger":"OrderService"`) {
		t.Errorf("json stream missing logger field: %s", out)
	}
	if !strings.Contains(out, `logger=OrderService`) {
		t.Errorf("text stream missing logger field: %s", out)
	}
}

// Source attribution points at the caller. The guard is that source.function
// and source.file identify the calling test: a wrong frame count would land
// inside log/slog or utils/log. (We assert the function and file rather than
// an exact line, which would rot on any edit to this file.)
func TestAttributionPointsAtCallSite(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	log.GetLoggerTo("AttributionTest", &buf).Info("attributed")

	var got []attributedLine
	for l := range strings.SplitSeq(buf.String(), "\n") {
		var m struct {
			Msg    string `json:"msg"`
			Source struct {
				Function string `json:"function"`
				File     string `json:"file"`
				Line     int    `json:"line"`
			} `json:"source"`
		}
		if !strings.Contains(l, `"msg"`) {
			continue
		}
		if err := json.Unmarshal([]byte(l), &m); err != nil {
			t.Fatalf("json parse: %v\n%s", err, l)
		}
		got = append(got, attributedLine{m.Source.Function, m.Source.File, m.Source.Line})
	}
	if len(got) == 0 {
		t.Fatalf("no attributed event captured:\n%s", buf.String())
	}
	for _, a := range got {
		if a.function != "github.com/dfryer1193/mjolnir/utils/log_test.TestAttributionPointsAtCallSite" {
			t.Fatalf("source function is not the caller (wrong frame count?): %s", a.function)
		}
		if !strings.HasSuffix(a.file, "utils/log/logger_test.go") {
			t.Fatalf("source file is not this test file (wrong frame count?): %s", a.file)
		}
		if a.line <= 0 {
			t.Fatalf("source line is not a valid line number: %d", a.line)
		}
	}
}

type attributedLine struct {
	function string
	file     string
	line     int
}
