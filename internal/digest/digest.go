package digest

import (
	"context"
	"fmt"
	"log"
	"strconv"
	"strings"
	"time"

	"monitoring-master/internal/models"
	"monitoring-master/internal/repository"
	"monitoring-master/internal/services"
)

// digestCatchUpWindow is how long after the configured time the daily digest may
// still be dispatched. Covers a master restart or a missed scheduler tick without
// letting a late boot push the report far past its slot.
const digestCatchUpWindow = 15 * time.Minute

// ParseDigestTime parses a configured digest time. "HH:MM" is the canonical form;
// "H:MM" and "HH:MM:SS" are accepted so legacy or hand-edited values still schedule.
func ParseDigestTime(value string) (hour, minute int, ok bool) {
	parts := strings.Split(strings.TrimSpace(value), ":")
	if len(parts) < 2 || len(parts) > 3 {
		return 0, 0, false
	}
	hour, errHour := strconv.Atoi(strings.TrimSpace(parts[0]))
	minute, errMinute := strconv.Atoi(strings.TrimSpace(parts[1]))
	if errHour != nil || errMinute != nil || hour < 0 || hour > 23 || minute < 0 || minute > 59 {
		return 0, 0, false
	}
	return hour, minute, true
}

// ShouldSendDigest reports whether today's digest is due: the configured time has
// been reached (within the catch-up window) and nothing was sent yet today.
// lastSentDate is the "2006-01-02" date of the previous dispatch, empty on boot.
func ShouldSendDigest(now time.Time, digestTime, lastSentDate string) bool {
	if strings.TrimSpace(digestTime) == "" {
		digestTime = "08:00"
	}
	hour, minute, ok := ParseDigestTime(digestTime)
	if !ok {
		return false
	}
	target := time.Date(now.Year(), now.Month(), now.Day(), hour, minute, 0, 0, now.Location())
	if now.Before(target) || now.After(target.Add(digestCatchUpWindow)) {
		return false
	}
	return now.Format("2006-01-02") != lastSentDate
}

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
	return BuildInstitutionDigestMessageWithServices(instName, isOnline, totalServices, runningServices, downServices, nil, incidents24h, t)
}

// BuildInstitutionDigestMessageWithServices formats a markdown summary for a single institution, including down service display names.
func BuildInstitutionDigestMessageWithServices(instName string, isOnline bool, totalServices, runningServices, downServices int, downServiceList []models.Service, incidents24h int64, t time.Time) string {
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

	msg := fmt.Sprintf("📊 *LAPORAN HARIAN LEMBAGA*\n\n"+
		"🏢 *Lembaga:* %s\n"+
		"⏰ *Waktu:* %s\n\n"+
		"🖥️ *Node Server:* %s\n"+
		"⚙️ *Kondisi Service:* %s (%d/%d Aktif)\n"+
		"%s *Insiden 24 Jam:* %d insiden\n\n",
		instName,
		t.Format("02 Jan 2006 15:04 WIB"),
		nodeStr,
		svcStr, runningServices, totalServices,
		incidentIcon,
		incidents24h,
	)

	if len(downServiceList) > 0 {
		msg += "_Service Masih Bermasalah:_\n"
		for i, svc := range downServiceList {
			name := svc.GetDisplayName()
			msg += fmt.Sprintf("%d. ⚠️ %s\n", i+1, name)
		}
		msg += "\n"
	}

	msg += "_Sistem monitoring beroperasi normal._"
	return msg
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
		downList     []models.Service
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

		var instDownList []models.Service
		for _, s := range servicesList {
			switch s.Status {
			case "UP":
				instRunningSvc++
			case "DOWN":
				instDownSvc++
				instDownList = append(instDownList, s)
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
			downList:     instDownList,
			incidents24h: instIncidents,
		})
	}

	totalIncidents24h, _ := repo.GetRecentIncidentsCount(ctx, since24h)

	sentCount := 0

	// 2. Dispatch to Global Admin group if configured
	if setting.TelegramBotToken != "" && setting.TelegramChatID != "" {
		globalMsg := BuildGlobalDigestMessage(totalNodes, onlineNodes, totalServices, runningServices, downServices, totalIncidents24h, now)
		_, err := services.SendTelegramAlert(setting.TelegramBotToken, setting.TelegramChatID, setting.TelegramTopicID, globalMsg)
		if err != nil {
			log.Printf("[DAILY DIGEST ERROR] Failed to send global digest: %v", err)
		} else {
			sentCount++
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

			instMsg := BuildInstitutionDigestMessageWithServices(inst.Name, stat.isOnline, stat.totalSvc, stat.runningSvc, stat.downSvc, stat.downList, stat.incidents24h, now)
			_, err := services.SendTelegramAlert(inst.TelegramBotToken, inst.TelegramChatID, inst.TelegramTopicID, instMsg)
			if err != nil {
				log.Printf("[DAILY DIGEST ERROR] Failed to send custom digest to %s: %v", inst.Name, err)
			} else {
				sentCount++
				log.Printf("[DAILY DIGEST] Sent custom digest to %s (chat %s)", inst.Name, inst.TelegramChatID)
			}
		}
	}

	if sentCount == 0 {
		return fmt.Errorf("no telegram destination configured or every dispatch failed (global bot/chat unset and no institution uses a custom bot)")
	}

	return nil
}
