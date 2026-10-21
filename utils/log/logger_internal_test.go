package log

import (
	"bytes"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// These tests exercise the stateful machinery — sink swapping, the level
// table, and SetJSONFile — against a core constructed per test. Nothing here
// touches defaultCore, so the whole file runs in parallel. The exported
// functions are thin delegates to the same core methods.

// newCapturedCore returns an isolated core whose streams both write to buf.
func newCapturedCore(t *testing.T) (*core, *bytes.Buffer) {
	t.Helper()
	c := newCore(io.Discard, io.Discard)
	var buf bytes.Buffer
	t.Cleanup(c.useOutputForTests(&buf))
	return c, &buf
}

// The capture seam redirects both sinks, and its restore puts back the
// previous destination. Verified by nesting: after restoring the inner
// redirect, output must land in the outer buffer rather than the core's
// original sinks.
func TestUseOutputForTestsCapturesAndRestores(t *testing.T) {
	t.Parallel()
	c := newCore(io.Discard, io.Discard)
	var outer, inner bytes.Buffer
	lg := c.logger("CaptureTest")

	restoreOuter := c.useOutputForTests(&outer)
	t.Cleanup(restoreOuter)

	lg.Info("outer-captured")
	if !strings.Contains(outer.String(), "outer-captured") {
		t.Fatalf("expected outer capture, got %q", outer.String())
	}

	restoreInner := c.useOutputForTests(&inner)
	lg.Info("inner-captured")
	if !strings.Contains(inner.String(), "inner-captured") {
		t.Fatalf("expected inner capture, got %q", inner.String())
	}

	restoreInner()
	lg.Info("back-to-outer")
	if !strings.Contains(outer.String(), "back-to-outer") {
		t.Fatalf("restore did not return output to the outer sink: %q", outer.String())
	}
	if strings.Contains(inner.String(), "back-to-outer") {
		t.Fatalf("output leaked into the inner sink after restore: %q", inner.String())
	}
}

// The default level is a minimum threshold.
func TestDefaultLevelIsMinimumThreshold(t *testing.T) {
	t.Parallel()
	c, buf := newCapturedCore(t)

	lg := c.logger("Threshold")
	lg.Debug("debug-suppressed")
	lg.Info("info-emitted")
	lg.Warn("warn-emitted")
	lg.Error("error-emitted")

	out := buf.String()
	if strings.Contains(out, "debug-suppressed") {
		t.Errorf("debug emitted despite default Info threshold: %s", out)
	}
	for _, want := range []string{"info-emitted", "warn-emitted", "error-emitted"} {
		if !strings.Contains(out, want) {
			t.Errorf("%s not emitted: %s", want, out)
		}
	}
}

// Raising the default level takes effect immediately.
func TestSetDefaultLevelTakesEffectImmediately(t *testing.T) {
	t.Parallel()
	c, buf := newCapturedCore(t)

	lg := c.logger("DefaultLevelChange")
	c.setDefaultLevel(slog.LevelWarn)

	lg.Info("suppressed-info")
	lg.Warn("warn-still-emitted")

	out := buf.String()
	if strings.Contains(out, "suppressed-info") {
		t.Errorf("info emitted after SetDefaultLevel(Warn): %s", out)
	}
	if !strings.Contains(out, "warn-still-emitted") {
		t.Errorf("warn not emitted after SetDefaultLevel(Warn): %s", out)
	}
}

// A per-name level applies to that name only; two names with different
// explicit levels behave independently.
func TestSetLevelIsPerName(t *testing.T) {
	t.Parallel()
	c := newCore(io.Discard, io.Discard)
	var a, b bytes.Buffer
	c.setLevel("AVerbose", slog.LevelDebug)
	c.setLevel("BQuiet", slog.LevelError)

	c.loggerTo("AVerbose", &a).Debug("a-debug")
	c.loggerTo("BQuiet", &b).Debug("b-debug")

	if !strings.Contains(a.String(), "a-debug") {
		t.Errorf("AVerbose debug not emitted: %s", a.String())
	}
	if strings.Contains(b.String(), "b-debug") {
		t.Errorf("BQuiet debug should be suppressed at Error: %s", b.String())
	}
}

// A level change reaches a held logger, both directions.
func TestLevelChangeReachesHeldLogger(t *testing.T) {
	t.Parallel()
	c := newCore(io.Discard, io.Discard)
	var buf bytes.Buffer
	lg := c.loggerTo("Held", &buf)

	c.setLevel("Held", slog.LevelDebug)
	lg.Debug("on")
	c.setLevel("Held", slog.LevelError)
	lg.Debug("off")

	out := buf.String()
	if !strings.Contains(out, "on") {
		t.Errorf("debug not emitted after SetLevel(Debug): %s", out)
	}
	if strings.Contains(out, "off") {
		t.Errorf("debug emitted after SetLevel(Error): %s", out)
	}
}

// A level change reaches With()-derived loggers too.
func TestLevelChangeReachesDerivedLogger(t *testing.T) {
	t.Parallel()
	c := newCore(io.Discard, io.Discard)
	var buf bytes.Buffer
	child := c.loggerTo("Derived", &buf).With("component", "child")

	c.setLevel("Derived", slog.LevelDebug)
	child.Debug("on")
	c.setLevel("Derived", slog.LevelError)
	child.Debug("off")

	out := buf.String()
	if !strings.Contains(out, "on") {
		t.Errorf("derived debug not emitted after SetLevel(Debug): %s", out)
	}
	if strings.Contains(out, "off") {
		t.Errorf("derived debug emitted after SetLevel(Error): %s", out)
	}
}

// SetLevel before GetLogger: a later logger emits at it.
func TestSetLevelBeforeGetLogger(t *testing.T) {
	t.Parallel()
	c := newCore(io.Discard, io.Discard)
	var buf bytes.Buffer
	c.setLevel("Future", slog.LevelDebug)
	c.loggerTo("Future", &buf).Debug("emitted-at-future-debug")

	if !strings.Contains(buf.String(), "emitted-at-future-debug") {
		t.Errorf("logger created after SetLevel(Debug) did not emit at debug: %s", buf.String())
	}
}

// Concurrent emission and level changes are race-free. Run with -race; the
// race detector is the assertion.
func TestConcurrentEmissionAndLevelChange(t *testing.T) {
	t.Parallel()
	c := newCore(io.Discard, io.Discard)
	var buf bytes.Buffer
	held := c.loggerTo("Racy", &buf)
	derived := held.With("component", "child")

	const workers = 8
	var wg sync.WaitGroup
	stop := make(chan struct{})
	for range workers {
		wg.Go(func() {
			for {
				select {
				case <-stop:
					return
				default:
				}
				held.Info("tick")
				derived.Debug("child-tick")
			}
		})
	}
	wg.Go(func() {
		for {
			select {
			case <-stop:
				return
			default:
			}
			c.setLevel("Racy", slog.LevelDebug)
			c.setLevel("Racy", slog.LevelInfo)
		}
	})

	time.Sleep(100 * time.Millisecond)
	close(stop)
	wg.Wait()
}

// SetJSONFile redirects the JSON stream to a real file.
func TestSetJSONFileWritesJSON(t *testing.T) {
	t.Parallel()
	var text bytes.Buffer
	c := newCore(&text, io.Discard)

	path := filepath.Join(t.TempDir(), "nested", "app.jsonl")
	if err := c.setJSONFile(path); err != nil {
		t.Fatalf("setJSONFile: %v", err)
	}

	c.logger("JSONFile").Info("in-file")

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading json file: %v", err)
	}
	if !strings.Contains(string(data), `"msg":"in-file"`) || !strings.Contains(string(data), `"logger":"JSONFile"`) {
		t.Fatalf("json file missing event: %s", data)
	}
	if !strings.Contains(text.String(), "in-file") {
		t.Fatalf("text stream missing event: %s", text.String())
	}
}

// An unopenable path errors and leaves the JSON sink intact.
func TestSetJSONFileErrorLeavesSinkIntact(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	blocker := filepath.Join(dir, "blocker")
	if err := os.WriteFile(blocker, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}

	var text, jsonBuf bytes.Buffer
	c := newCore(&text, &jsonBuf)
	if err := c.setJSONFile(filepath.Join(blocker, "x.jsonl")); err == nil {
		t.Fatal("expected error for unopenable path")
	}

	c.logger("AfterErr").Info("still-works")
	if !strings.Contains(text.String(), "still-works") {
		t.Fatalf("text stream broken after failed SetJSONFile: %s", text.String())
	}
	if !strings.Contains(jsonBuf.String(), "still-works") {
		t.Fatalf("json sink changed after failed SetJSONFile: %s", jsonBuf.String())
	}
}
