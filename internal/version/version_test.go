package version

import (
	"bytes"
	"log"
	"os"
	"strings"
	"testing"
)

func TestVersionGet(t *testing.T) {
	info := Get()
	if info.AppName != "Service Monitor Master" {
		t.Errorf("expected AppName 'Service Monitor Master', got '%s'", info.AppName)
	}
	if info.Version == "" {
		t.Error("expected non-empty Version")
	}
	if info.GoVersion == "" {
		t.Error("expected non-empty GoVersion")
	}
	if info.PID <= 0 {
		t.Errorf("expected PID > 0, got %d", info.PID)
	}
	if info.OS == "" || info.Arch == "" {
		t.Errorf("expected OS and Arch to be set, got OS=%s, Arch=%s", info.OS, info.Arch)
	}
}

func TestVersionShort(t *testing.T) {
	short := Short()
	if !strings.Contains(short, "Service Monitor Master") {
		t.Errorf("expected short version string to contain AppName, got '%s'", short)
	}
	if !strings.Contains(short, Version) {
		t.Errorf("expected short version string to contain '%s', got '%s'", Version, short)
	}
}

func TestLogStartupBanner(t *testing.T) {
	var buf bytes.Buffer
	origWriter := log.Writer()
	origFlags := log.Flags()
	defer func() {
		log.SetOutput(origWriter)
		log.SetFlags(origFlags)
	}()

	log.SetOutput(&buf)
	log.SetFlags(0)

	LogStartupBanner("8080", "mongo", "release", false)

	output := buf.String()
	if !strings.Contains(output, "Service Monitor Master") {
		t.Errorf("expected banner output to contain AppName, got:\n%s", output)
	}
	if !strings.Contains(output, "8080") {
		t.Errorf("expected banner output to contain port '8080', got:\n%s", output)
	}
	if !strings.Contains(output, "mongo") {
		t.Errorf("expected banner output to contain dbDriver 'mongo', got:\n%s", output)
	}
	if !strings.Contains(output, "release") {
		t.Errorf("expected banner output to contain ginMode 'release', got:\n%s", output)
	}
	if !strings.Contains(output, "Debug Log") {
		t.Errorf("expected banner output to contain 'Debug Log', got:\n%s", output)
	}
	if !strings.Contains(output, os.Getenv("PID")) && !strings.Contains(output, "Process PID") {
		t.Errorf("expected banner output to contain Process PID, got:\n%s", output)
	}
}
