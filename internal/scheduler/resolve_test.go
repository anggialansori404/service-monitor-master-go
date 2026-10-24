package scheduler

import (
	"strings"
	"testing"

	"monitoring-master/internal/models"
)

// Mirrors the reported case: a schedule holding both Name and DisplayName for
// every service (8 entries for 4 services). Only Name is resolvable by the agent.
func TestResolveServiceNames_CampuranNameDanDisplayName(t *testing.T) {
	services := []models.Service{
		{Name: "otorisator", DisplayName: "Otorisator Service"},
		{Name: "ibs-branchless", DisplayName: "IBS Branchless Service"},
		{Name: "qris-bpd-bali", DisplayName: "QRIS BPD Bali Service"},
		{Name: "wafins", DisplayName: "Wafins Service"},
	}
	entries := []string{
		"otorisator", "Otorisator Service",
		"ibs-branchless", "IBS Branchless Service",
		"qris-bpd-bali", "QRIS BPD Bali Service",
		"wafins", "Wafins Service",
	}

	resolved, unknown := ResolveServiceNames(entries, services)

	if len(unknown) != 0 {
		t.Fatalf("expected no unknown entries, got %v", unknown)
	}
	want := []string{"otorisator", "ibs-branchless", "qris-bpd-bali", "wafins"}
	if strings.Join(resolved, ",") != strings.Join(want, ",") {
		t.Fatalf("expected %v, got %v", want, resolved)
	}
}

func TestResolveServiceNames_DisplayNameSaja(t *testing.T) {
	services := []models.Service{{Name: "otorisator", DisplayName: "Otorisator Service"}}

	resolved, unknown := ResolveServiceNames([]string{"Otorisator Service"}, services)
	if len(unknown) != 0 {
		t.Fatalf("expected display_name to resolve, got unknown %v", unknown)
	}
	if len(resolved) != 1 || resolved[0] != "otorisator" {
		t.Fatalf("expected [otorisator], got %v", resolved)
	}
}

func TestResolveServiceNames_TidakDikenalDilaporkan(t *testing.T) {
	services := []models.Service{{Name: "nginx", DisplayName: "Web Server"}}

	resolved, unknown := ResolveServiceNames([]string{"nginx", "tidak-ada"}, services)
	if len(resolved) != 1 || resolved[0] != "nginx" {
		t.Fatalf("expected [nginx], got %v", resolved)
	}
	if len(unknown) != 1 || unknown[0] != "tidak-ada" {
		t.Fatalf("expected unknown [tidak-ada], got %v", unknown)
	}
}

func TestResolveServiceNames_TanpaKatalogLolosApaAdanya(t *testing.T) {
	resolved, unknown := ResolveServiceNames([]string{"nginx", "nginx"}, nil)
	if len(unknown) != 0 {
		t.Fatalf("expected no unknown entries, got %v", unknown)
	}
	if len(resolved) != 1 || resolved[0] != "nginx" {
		t.Fatalf("expected deduped [nginx], got %v", resolved)
	}
}
