package deploy

import (
	"log"
	"path/filepath"
	"strings"
)

// logOutput logs script output line by line, prefixed with the script name,
// so that output cannot be mistaken for deploy-gate's own log lines.
func logOutput(script, output string) {
	if output == "" {
		return
	}
	name := filepath.Base(script)
	for _, line := range strings.Split(strings.TrimSuffix(output, "\n"), "\n") {
		log.Printf("[%s] %s", name, line)
	}
}
