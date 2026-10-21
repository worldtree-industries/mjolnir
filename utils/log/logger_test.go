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

// Sensitive keys are masked in both streams; others pass.
func TestRedactionMasksSensitiveKeys(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	lg := log.GetLoggerTo("RedactionTest", &buf)
	lg.Info("login",
		"password", "s3cret",
		"authorization", "Bearer token123",
		"email", "user@example.com",
		"username", "alice",
	)

	out := buf.String()
	for _, secret := range []string{"s3cret", "token123", "user@example.com"} {
		if strings.Contains(out, secret) {
			t.Errorf("secret leaked: %q in %s", secret, out)
		}
	}
	if !strings.Contains(out, `"password":"***"`) {
		t.Errorf("password not masked in json: %s", out)
	}
	if !strings.Contains(out, `password=***`) {
		t.Errorf("password not masked in text: %s", out)
	}
	if !strings.Contains(out, `"username":"alice"`) {
		t.Errorf("non-sensitive key was altered: %s", out)
	}
}

// Redaction is case-insensitive on the key.
func TestRedactionIsCaseInsensitive(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	lg := log.GetLoggerTo("RedactionCase", &buf)
	lg.Info("login", "Password", "hunter2", "EMAIL", "x@y.z")

	out := buf.String()
	if strings.Contains(out, "hunter2") || strings.Contains(out, "x@y.z") {
		t.Errorf("case-variant sensitive keys not masked: %s", out)
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
