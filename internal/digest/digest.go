package digest

import (
	"context"
	"fmt"
	"log"
	"time"

	"monitoring-master/internal/models"
	"monitoring-master/internal/repository"
	"monitoring-master/internal/services"
)

// BuildGlobalDigestMessage formats a markdown summary of all institutions and services.
func BuildGlobalDigestMessage(totalNodes, onlineNodes, totalServices, runningServices, downServices int, incidents24h int64, t time.Time) string {
	nodeStatus := "🟢 Semua Node Online"
	if onlineNodes < totalNodes {
		nodeStatus = fmt.Sprintf("⚠️ %d/%d Node Online (%d Offline)", onlineNodes, totalNodes, totalNodes-onlineNodes)
	}

	serviceStatus := "🟢 Semua Service Berjalan Normal"
	if downServices > 0 {
		serviceStatus = fmt.Sprintf("🔴 %d Service Mengalami Masalah (DOWN)", downServices)
	}

	incidentStatus := "✅ Tidak ada insiden dalam 24 jam terakhir"
	if incidents24h > 0 {
		incidentStatus = fmt.Sprintf("🚨 Terjadi %d insiden dalam 24 jam terakhir", incidents24h)
	}

	return fmt.Sprintf("📊 *LAPORAN MONITORING HARIAN*\n\n"+
		"🏢 *Cakupan:* Seluruh Lembaga Terhubung\n"+
		"⏰ *Waktu:* %s\n\n"+
		"🖥️ *Status Node:* %s\n"+
		"⚙️ *Status Service:* %s (%d Running, %d Down)\n"+
		"🛡️ *Riwayat Insiden:* %s\n\n"+
		"_Laporan otomatis harian dari Service Monitor Hub._",
		t.Format("02 Jan 2006 15:04 WIB"),
		nodeStatus,
		serviceStatus, runningServices, downServices,
		incidentStatus,
	)
}

// BuildInstitutionDigestMessage formats a markdown summary for a single institution.
func BuildInstitutionDigestMessage(instName string, isOnline bool, totalServices, runningServices, downServices int, incidents24h int64, t time.Time) string {
	nodeStr := "🟢 Online (Tersambung)"
	if !isOnline {
		nodeStr = "🔴 Offline (Terputus)"
	}

	svcStr := "🟢 Semua Berjalan Normal"
	if downServices > 0 {
		svcStr = fmt.Sprintf("⚠️ %d Service DOWN", downServices)
	}

	incidentIcon := "✅"
	if incidents24h > 0 {
		incidentIcon = "🚨"
	}

	return fmt.Sprintf("📊 *LAPORAN HARIAN LEMBAGA*\n\n"+
		"🏢 *Lembaga:* %s\n"+
		"⏰ *Waktu:* %s\n\n"+
		"🖥️ *Node Server:* %s\n"+
		"⚙️ *Kondisi Service:* %s (%d/%d Aktif)\n"+
		"%s *Insiden 24 Jam:* %d insiden\n\n"+
		"_Sistem monitoring beroperasi normal._",
		instName,
		t.Format("02 Jan 2006 15:04 WIB"),
		nodeStr,
		svcStr, runningServices, totalServices,
		incidentIcon,
		incidents24h,
	)
}

// SendDailyDigest compiles system health statistics and sends to Global and Custom Telegram destinations.
func SendDailyDigest(ctx context.Context, repo repository.Repository) error {
	setting, err := repo.GetSettings(ctx)
	if err != nil {
		return fmt.Errorf("fetch settings: %w", err)
	}

	if !setting.EnableDailyDigest {
		log.Println("[DAILY DIGEST] Skipped: EnableDailyDigest is disabled in settings.")
		return nil
	}

	now := time.Now().Local()
	since24h := now.Add(-24 * time.Hour)

	// 1. Fetch all institutions
	institutions, err := repo.GetAllInstitutions()
	if err != nil {
		return fmt.Errorf("fetch institutions: %w", err)
	}

	totalNodes := len(institutions)
	onlineNodes := 0
	totalServices := 0
	runningServices := 0
	downServices := 0

	type instStat struct {
		inst         models.Institution
		isOnline     bool
		totalSvc     int
		runningSvc   int
		downSvc      int
		incidents24h int64
	}
	var instStats []instStat

	for _, inst := range institutions {
		isOnline := false
		if inst.LastPing != nil && time.Since(*inst.LastPing) < 3*time.Minute {
			isOnline = true
			onlineNodes++
		}

		servicesList, _, _ := repo.GetServicesByInstitution(inst.ID, "", "", 1, 500)
		instTotalSvc := len(servicesList)
		instRunningSvc := 0
		instDownSvc := 0

		for _, s := range servicesList {
			switch s.Status {
			case "UP":
				instRunningSvc++
			case "DOWN":
				instDownSvc++
			}
		}

		totalServices += instTotalSvc
		runningServices += instRunningSvc
		downServices += instDownSvc

		instIncidents, err := repo.GetRecentIncidentsCountByInstitution(ctx, inst.ID, since24h)
		if err != nil {
			log.Printf("[DAILY DIGEST ERROR] Failed to fetch incidents count for %s: %v", inst.Name, err)
			instIncidents = 0
		}

		instStats = append(instStats, instStat{
			inst:         inst,
			isOnline:     isOnline,
			totalSvc:     instTotalSvc,
			runningSvc:   instRunningSvc,
			downSvc:      instDownSvc,
			incidents24h: instIncidents,
		})
	}

	totalIncidents24h, _ := repo.GetRecentIncidentsCount(ctx, since24h)

	// 2. Dispatch to Global Admin group if configured
	if setting.TelegramBotToken != "" && setting.TelegramChatID != "" {
		globalMsg := BuildGlobalDigestMessage(totalNodes, onlineNodes, totalServices, runningServices, downServices, totalIncidents24h, now)
		_, err := services.SendTelegramAlert(setting.TelegramBotToken, setting.TelegramChatID, setting.TelegramTopicID, globalMsg)
		if err != nil {
			log.Printf("[DAILY DIGEST ERROR] Failed to send global digest: %v", err)
		} else {
			log.Println("[DAILY DIGEST] Successfully sent global daily digest.")
		}
	}

	// 3. Dispatch to Custom Institution Bots
	for _, stat := range instStats {
		inst := stat.inst
		if inst.UseCustomTelegram && inst.TelegramBotToken != "" && inst.TelegramChatID != "" {
			// Skip if institution is in maintenance mode
			if inst.InMaintenance && (inst.MaintenanceUntil == nil || time.Now().Before(*inst.MaintenanceUntil)) {
				continue
			}

			instMsg := BuildInstitutionDigestMessage(inst.Name, stat.isOnline, stat.totalSvc, stat.runningSvc, stat.downSvc, stat.incidents24h, now)
			_, err := services.SendTelegramAlert(inst.TelegramBotToken, inst.TelegramChatID, inst.TelegramTopicID, instMsg)
			if err != nil {
				log.Printf("[DAILY DIGEST ERROR] Failed to send custom digest to %s: %v", inst.Name, err)
			} else {
				log.Printf("[DAILY DIGEST] Sent custom digest to %s (chat %s)", inst.Name, inst.TelegramChatID)
			}
		}
	}

	return nil
}
