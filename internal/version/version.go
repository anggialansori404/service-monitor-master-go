package version

import (
	"fmt"
	"log"
	"os"
	"runtime"
	"runtime/debug"
	"strings"
	"time"
)

// Version info variables (can be overridden during build with -ldflags)
var (
	Version   = "v1.2.1"
	AppName   = "Service Monitor Master"
	BuildTime = ""
	GitCommit = ""
)

func init() {
	// Auto-extract VCS metadata from Go module buildinfo if not injected via -ldflags
	if info, ok := debug.ReadBuildInfo(); ok {
		var isDirty bool
		for _, setting := range info.Settings {
			switch setting.Key {
			case "vcs.revision":
				if GitCommit == "" && setting.Value != "" {
					GitCommit = setting.Value
					if len(GitCommit) > 7 {
						GitCommit = GitCommit[:7]
					}
				}
			case "vcs.time":
				if BuildTime == "" && setting.Value != "" {
					if t, err := time.Parse(time.RFC3339, setting.Value); err == nil {
						BuildTime = t.Local().Format("2006-01-02 15:04:05")
					} else {
						BuildTime = setting.Value
					}
				}
			case "vcs.modified":
				if setting.Value == "true" {
					isDirty = true
				}
			}
		}
		if isDirty && GitCommit != "" && !strings.HasSuffix(GitCommit, "-dirty") {
			GitCommit += "-dirty"
		}
	}
}

// Info holds consolidated version and runtime metadata
type Info struct {
	AppName   string `json:"app_name"`
	Version   string `json:"version"`
	BuildTime string `json:"build_time,omitempty"`
	GitCommit string `json:"git_commit,omitempty"`
	GoVersion string `json:"go_version"`
	OS        string `json:"os"`
	Arch      string `json:"arch"`
	NumCPU    int    `json:"num_cpu"`
	PID       int    `json:"pid"`
}

// Get returns the consolidated runtime and version metadata
func Get() Info {
	return Info{
		AppName:   AppName,
		Version:   Version,
		BuildTime: BuildTime,
		GitCommit: GitCommit,
		GoVersion: runtime.Version(),
		OS:        runtime.GOOS,
		Arch:      runtime.GOARCH,
		NumCPU:    runtime.NumCPU(),
		PID:       os.Getpid(),
	}
}

// LogStartupBanner prints a clean, formatted startup header to standard logs
func LogStartupBanner(port, dbDriver, ginMode string, debugLog bool) {
	info := Get()
	border := strings.Repeat("=", 60)

	log.Println(border)
	log.Printf(">>> %s (%s)", info.AppName, info.Version)
	log.Println(border)
	log.Printf(" - Go Runtime  : %s (%s/%s, %d CPUs)", info.GoVersion, info.OS, info.Arch, info.NumCPU)
	log.Printf(" - Process PID : %d", info.PID)
	log.Printf(" - Timezone    : %s (UTC%s)", time.Local.String(), time.Now().Format("-07:00"))
	log.Printf(" - Database    : %s", dbDriver)
	log.Printf(" - Server Port : :%s", port)
	log.Printf(" - Gin Mode    : %s", ginMode)
	log.Printf(" - Debug Log   : %v", debugLog)
	if info.GitCommit != "" {
		log.Printf(" - Git Commit  : %s", info.GitCommit)
	}
	if info.BuildTime != "" {
		log.Printf(" - Build Time  : %s", info.BuildTime)
	}
	log.Println(border)
}

// Short returns a concise version string e.g. "Service Monitor Master v1.2.0 (go1.25.5)"
func Short() string {
	return fmt.Sprintf("%s %s (%s)", AppName, Version, runtime.Version())
}
