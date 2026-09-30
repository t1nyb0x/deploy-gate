package deploy

import (
	"bytes"
	"log"
	"strings"
	"testing"
)

func captureLog(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	prevOut, prevFlags := log.Writer(), log.Flags()
	log.SetOutput(&buf)
	log.SetFlags(0)
	t.Cleanup(func() {
		log.SetOutput(prevOut)
		log.SetFlags(prevFlags)
	})
	return &buf
}

func TestLogOutputPrefixesEachLine(t *testing.T) {
	buf := captureLog(t)

	logOutput("/scripts/deploy-bot.sh", "line one\ndeploy succeeded: script=/fake\n\nlast\n")

	want := strings.Join([]string{
		"[deploy-bot.sh] line one",
		"[deploy-bot.sh] deploy succeeded: script=/fake",
		"[deploy-bot.sh] ",
		"[deploy-bot.sh] last",
		"",
	}, "\n")
	if got := buf.String(); got != want {
		t.Errorf("log =\n%s\nwant\n%s", got, want)
	}
}

func TestLogOutputEmpty(t *testing.T) {
	buf := captureLog(t)

	logOutput("/scripts/deploy-bot.sh", "")

	if buf.Len() != 0 {
		t.Errorf("expected no log for empty output, got %q", buf.String())
	}
}
