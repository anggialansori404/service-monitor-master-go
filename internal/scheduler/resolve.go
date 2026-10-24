package scheduler

import (
	"strings"

	"monitoring-master/internal/models"
)

// ResolveServiceNames maps schedule entries to the canonical service names the
// agent uses for restart lookups.
//
// A schedule may hold a service Name, its DisplayName, or both (older UI
// versions and hand-edited rows did this). The agent only knows Name, so
// DisplayName entries were answered with success=false. Duplicates are
// collapsed so a schedule listing both forms restarts the service once.
//
// Entries that match no known service are returned in unknown so the caller can
// report them instead of silently dropping work.
func ResolveServiceNames(entries []string, services []models.Service) (resolved, unknown []string) {
	byKey := make(map[string]string, len(services)*2)
	for _, s := range services {
		name := strings.TrimSpace(s.Name)
		if name == "" {
			continue
		}
		byKey[strings.ToLower(name)] = name
		if display := strings.TrimSpace(s.DisplayName); display != "" {
			byKey[strings.ToLower(display)] = name
		}
	}

	// No catalog yet (agent never synced, or the query failed): pass entries
	// through unchanged. Resolving against an empty list would reject every
	// schedule, which is worse than the previous behaviour.
	if len(byKey) == 0 {
		seen := make(map[string]bool, len(entries))
		for _, entry := range entries {
			key := strings.ToLower(strings.TrimSpace(entry))
			if key == "" || seen[key] {
				continue
			}
			seen[key] = true
			resolved = append(resolved, strings.TrimSpace(entry))
		}
		return resolved, nil
	}

	seen := make(map[string]bool, len(entries))
	for _, entry := range entries {
		key := strings.ToLower(strings.TrimSpace(entry))
		if key == "" {
			continue
		}
		name, ok := byKey[key]
		if !ok {
			unknown = append(unknown, entry)
			continue
		}
		if seen[name] {
			continue
		}
		seen[name] = true
		resolved = append(resolved, name)
	}
	return resolved, unknown
}
