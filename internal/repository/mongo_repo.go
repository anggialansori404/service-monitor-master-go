package repository

import (
	"context"
	"fmt"
	"strings"
	"time"

	"monitoring-master/internal/models"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// MongoRepository implements Repository using the MongoDB v2 driver.
type MongoRepository struct {
	db *mongo.Database
}

// NewMongoRepository creates a new MongoRepository.
func NewMongoRepository(db *mongo.Database) *MongoRepository {
	return &MongoRepository{db: db}
}

// --- Helper Functions ---

func parseObjectID(id string) (bson.ObjectID, error) {
	if id == "" {
		return bson.NilObjectID, nil
	}
	return bson.ObjectIDFromHex(id)
}

type mongoInstitutionDoc struct {
	ID                   interface{}          `bson:"_id"`
	Name                 string               `bson:"name"`
	AgentID              string               `bson:"agent_id"`
	SecretKey            string               `bson:"secret_key"`
	IsActive             bool                 `bson:"is_active"`
	LastPing             *time.Time           `bson:"last_ping,omitempty"`
	HardwareInfo         *models.HardwareInfo `bson:"hardware_info,omitempty"`
	UseCustomTelegram    bool                 `bson:"use_custom_telegram"`
	TelegramBotToken     string               `bson:"telegram_bot_token"`
	TelegramChatID       string               `bson:"telegram_chat_id"`
	TelegramTopicID      string               `bson:"telegram_topic_id"`
	EnableTelegramAlerts bool                 `bson:"enable_telegram_alerts"`
	MirrorToGlobal       bool                 `bson:"mirror_to_global"`
	InMaintenance        bool                 `bson:"in_maintenance"`
	MaintenanceUntil     *time.Time           `bson:"maintenance_until,omitempty"`
}

func (doc *mongoInstitutionDoc) toModel() *models.Institution {
	var docID string
	if oid, ok := doc.ID.(bson.ObjectID); ok {
		docID = oid.Hex()
	} else if s, ok := doc.ID.(string); ok {
		docID = s
	} else if doc.ID != nil {
		docID = fmt.Sprintf("%v", doc.ID)
	}

	return &models.Institution{
		ID:                   docID,
		Name:                 doc.Name,
		AgentID:              doc.AgentID,
		SecretKey:            doc.SecretKey,
		IsActive:             doc.IsActive,
		LastPing:             doc.LastPing,
		HardwareInfo:         doc.HardwareInfo,
		UseCustomTelegram:    doc.UseCustomTelegram,
		TelegramBotToken:     doc.TelegramBotToken,
		TelegramChatID:       doc.TelegramChatID,
		TelegramTopicID:      doc.TelegramTopicID,
		EnableTelegramAlerts: doc.EnableTelegramAlerts,
		MirrorToGlobal:       doc.MirrorToGlobal,
		InMaintenance:        doc.InMaintenance,
		MaintenanceUntil:     doc.MaintenanceUntil,
	}
}

// --- Institution ---

func (r *MongoRepository) GetInstitutionByAgentID(ctx context.Context, agentID string) (*models.Institution, error) {
	coll := r.db.Collection("institutions")
	var doc mongoInstitutionDoc
	err := coll.FindOne(ctx, bson.M{"agent_id": agentID}).Decode(&doc)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return doc.toModel(), nil
}

func (r *MongoRepository) GetInstitutionByID(id string) (*models.Institution, error) {
	objID, _ := parseObjectID(id)
	coll := r.db.Collection("institutions")
	var doc mongoInstitutionDoc
	filter := bson.M{"$or": []bson.M{{"_id": id}, {"_id": objID}}}
	err := coll.FindOne(context.TODO(), filter).Decode(&doc)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return doc.toModel(), nil
}

func (r *MongoRepository) GetInstitutionByName(name string) (*models.Institution, error) {
	coll := r.db.Collection("institutions")
	var doc mongoInstitutionDoc
	err := coll.FindOne(context.TODO(), bson.M{"name": name}).Decode(&doc)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return doc.toModel(), nil
}

func (r *MongoRepository) GetAllInstitutions() ([]models.Institution, error) {
	var docs []mongoInstitutionDoc
	ctx := context.TODO()

	// 1. Ambil semua Institutions
	cursor, err := r.db.Collection("institutions").Find(ctx, bson.M{})
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	if err = cursor.All(ctx, &docs); err != nil {
		return nil, err
	}

	// Jika kosong, kembalikan array kosong agar tidak panic
	if len(docs) == 0 {
		return []models.Institution{}, nil
	}

	institutions := make([]models.Institution, len(docs))
	for i := range docs {
		institutions[i] = *docs[i].toModel()
	}

	// 2. Loop dan ambil Services untuk tiap Institution
	for i, inst := range institutions {
		var services []models.Service

		// Cari services yang institution_id nya sama dengan ID institution ini
		// Karena menggunakan manual fetch, kita gunakan ID dari inst yang sudah di-decode ke string atau fallback ke string
		filter := bson.M{"institution_id": inst.ID}

		srvCursor, err := r.db.Collection("services").Find(ctx, filter)
		if err == nil {
			srvCursor.All(ctx, &services)
			srvCursor.Close(ctx)
		}

		if services == nil {
			services = []models.Service{}
		}
		institutions[i].Services = services
	}

	return institutions, nil
}

func (r *MongoRepository) GetDashboardSummary(ctx context.Context) (*models.DashboardSummaryResponse, error) {
	// Dashboard Aggregation - Created by Kang Anggi
	coll := r.db.Collection("services")
	pipeline := mongo.Pipeline{
		{{Key: "$group", Value: bson.D{
			{Key: "_id", Value: nil},
			{Key: "total_services", Value: bson.D{{Key: "$sum", Value: 1}}},
			{Key: "total_up", Value: bson.D{{Key: "$sum", Value: bson.D{
				{Key: "$cond", Value: bson.A{bson.D{{Key: "$eq", Value: bson.A{"$status", "UP"}}}, 1, 0}},
			}}}},
			{Key: "total_down", Value: bson.D{{Key: "$sum", Value: bson.D{
				{Key: "$cond", Value: bson.A{bson.D{{Key: "$in", Value: bson.A{"$status", bson.A{"DOWN", "ERROR"}}}}, 1, 0}},
			}}}},
		}}},
	}

	cursor, err := coll.Aggregate(ctx, pipeline)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var result []struct {
		TotalServices int64 `bson:"total_services"`
		TotalUp       int64 `bson:"total_up"`
		TotalDown     int64 `bson:"total_down"`
	}

	if err := cursor.All(ctx, &result); err != nil {
		return nil, err
	}

	totalInstitutions, err := r.db.Collection("institutions").CountDocuments(ctx, bson.M{})
	if err != nil {
		return nil, err
	}

	summary := &models.DashboardSummaryResponse{
		TotalInstitutions: totalInstitutions,
	}

	if len(result) > 0 {
		summary.TotalServices = result[0].TotalServices
		summary.TotalUp = result[0].TotalUp
		summary.TotalDown = result[0].TotalDown
	}

	return summary, nil
}

func (r *MongoRepository) UpdateInstitutionPing(ctx context.Context, id string, hwInfo *models.HardwareInfo, t time.Time) error {
	objID, _ := parseObjectID(id)
	coll := r.db.Collection("institutions")

	updateFields := bson.M{"last_ping": t}
	if hwInfo != nil {
		updateFields["hardware_info"] = hwInfo
	}

	filter := bson.M{"$or": []bson.M{{"_id": id}, {"_id": objID}}}
	result, err := coll.UpdateOne(ctx, filter, bson.M{"$set": updateFields})
	if err != nil {
		return err
	}
	if result.MatchedCount == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *MongoRepository) CountInstitutions() (int64, error) {
	coll := r.db.Collection("institutions")
	return coll.CountDocuments(context.TODO(), bson.M{})
}

func (r *MongoRepository) CreateInstitution(inst *models.Institution) error {
	coll := r.db.Collection("institutions")
	if inst.ID == "" {
		inst.ID = bson.NewObjectID().Hex()
	}
	_, err := coll.InsertOne(context.TODO(), inst)
	return err
}

func (r *MongoRepository) UpdateInstitution(inst *models.Institution) error {
	objID, _ := parseObjectID(inst.ID)
	// First encode to string, re-decode to intermediate to handle ID overwrite if any. We will use a BSON mapping directly.
	mapped := bson.M{
		"name":                   inst.Name,
		"agent_id":               inst.AgentID,
		"secret_key":             inst.SecretKey,
		"is_active":              inst.IsActive,
		"last_ping":              inst.LastPing,
		"use_custom_telegram":    inst.UseCustomTelegram,
		"telegram_bot_token":     inst.TelegramBotToken,
		"telegram_chat_id":       inst.TelegramChatID,
		"telegram_topic_id":      inst.TelegramTopicID,
		"enable_telegram_alerts": inst.EnableTelegramAlerts,
		"mirror_to_global":       inst.MirrorToGlobal,
		"in_maintenance":         inst.InMaintenance,
		"maintenance_until":      inst.MaintenanceUntil,
	}

	coll := r.db.Collection("institutions")
	filter := bson.M{"$or": []bson.M{{"_id": inst.ID}, {"_id": objID}}}
	result, err := coll.UpdateOne(context.TODO(), filter, bson.M{"$set": mapped})
	if err != nil {
		return err
	}
	if result.MatchedCount == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *MongoRepository) DeleteInstitution(id string) error {
	objID, _ := parseObjectID(id)
	// Delete associated services
	servicesColl := r.db.Collection("services")
	_, err := servicesColl.DeleteMany(context.TODO(), bson.M{"institution_id": id}) // Note! Stored as string in service maybe? Or objID? Assuming string based on updated model
	if err != nil {
		return err
	}

	// Delete associated audit logs
	auditColl := r.db.Collection("audit_logs")
	_, err = auditColl.DeleteMany(context.TODO(), bson.M{"institution_id": id})
	if err != nil {
		return err
	}

	// Delete the institution
	coll := r.db.Collection("institutions")
	filter := bson.M{"$or": []bson.M{{"_id": id}, {"_id": objID}}}
	result, err := coll.DeleteOne(context.TODO(), filter)
	if err != nil {
		return err
	}
	if result.DeletedCount == 0 {
		return ErrNotFound
	}
	return nil
}

// --- Service ---

func (r *MongoRepository) GetServicesByInstitution(instID string, search string, status string, page, limit int) ([]models.Service, int64, error) {
	coll := r.db.Collection("services")
	objID, _ := parseObjectID(instID)
	filter := bson.M{"institution_id": bson.M{"$in": []interface{}{instID, objID}}}

	if search != "" {
		filter["name"] = bson.M{"$regex": search, "$options": "i"}
	}

	// Inject Status Filter - Created by Kang Anggi
	if status != "" && strings.ToUpper(status) != "ALL" {
		filter["status"] = strings.ToUpper(status)
	}

	total, err := coll.CountDocuments(context.TODO(), filter)
	if err != nil {
		return nil, 0, err
	}

	opts := options.Find().
		SetSkip(int64((page - 1) * limit)).
		SetLimit(int64(limit))

	cursor, err := coll.Find(context.TODO(), filter, opts)
	if err != nil {
		return nil, 0, err
	}
	defer cursor.Close(context.TODO())

	var docs []struct {
		ID            interface{} `bson:"_id"`
		InstitutionID interface{} `bson:"institution_id"`

		Name          string      `bson:"name"`
		Type          string      `bson:"type"`
		Status        string      `bson:"status"`
		AutoRestart   bool        `bson:"auto_restart"`
		RestartStatus string      `bson:"restart_status,omitempty"`
		LastChecked   *time.Time  `bson:"last_checked,omitempty"`
	}
	if err := cursor.All(context.TODO(), &docs); err != nil {
		return nil, 0, err
	}

	var services []models.Service
	for _, doc := range docs {
		var docID string
		if oid, ok := doc.ID.(bson.ObjectID); ok {
			docID = oid.Hex()
		} else if s, ok := doc.ID.(string); ok {
			docID = s
		}

		var instIDStr string
		if oid, ok := doc.InstitutionID.(bson.ObjectID); ok {
			instIDStr = oid.Hex()
		} else if s, ok := doc.InstitutionID.(string); ok {
			instIDStr = s
		}

		services = append(services, models.Service{
			ID:            docID,
			InstitutionID: instIDStr,
			Name:          doc.Name,
			Type:          doc.Type,
			Status:        doc.Status,
			AutoRestart:   doc.AutoRestart,
			RestartStatus: doc.RestartStatus,
			LastChecked:   doc.LastChecked,
		})
	}


	if services == nil {
		services = []models.Service{}
	}

	return services, total, nil
}

func (r *MongoRepository) GetService(instID string, name string) (*models.Service, error) {
	coll := r.db.Collection("services")
	var doc struct {
		ID            interface{} `bson:"_id"`
		InstitutionID string      `bson:"institution_id"`
		Name          string      `bson:"name"`
		Type          string      `bson:"type"`
		Status        string      `bson:"status"`
		AutoRestart   bool        `bson:"auto_restart"`
		RestartStatus string      `bson:"restart_status,omitempty"`
		LastChecked   *time.Time  `bson:"last_checked,omitempty"`
	}
	err := coll.FindOne(context.TODO(), bson.M{"institution_id": instID, "name": name}).Decode(&doc)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return nil, ErrNotFound
		}
		return nil, err
	}
	var docID string
	if oid, ok := doc.ID.(bson.ObjectID); ok {
		docID = oid.Hex()
	} else if s, ok := doc.ID.(string); ok {
		docID = s
	}
	service := models.Service{
		ID:            docID,
		InstitutionID: doc.InstitutionID,
		Name:          doc.Name,
		Type:          doc.Type,
		Status:        doc.Status,
		AutoRestart:   doc.AutoRestart,
		RestartStatus: doc.RestartStatus,
		LastChecked:   doc.LastChecked,
	}
	return &service, nil
}

func (r *MongoRepository) UpsertService(ctx context.Context, service *models.Service) error {
	coll := r.db.Collection("services")
	filter := bson.M{"institution_id": service.InstitutionID, "name": service.Name}

	var existing models.Service
	err := coll.FindOne(ctx, filter).Decode(&existing)
	if err != nil && err != mongo.ErrNoDocuments {
		return err
	}

	if err == mongo.ErrNoDocuments {
		if service.Status == "" {
			service.Status = "UP"
		}

		if service.ID == "" {
			service.ID = bson.NewObjectID().Hex()
		}
		_, insertErr := coll.InsertOne(ctx, service)
		return insertErr
	}

	statusStr := service.Status
	if statusStr == "" {
		statusStr = "UP"
	}

	update := bson.M{"$set": bson.M{
		"type":         service.Type,
		"status":       statusStr,
		"auto_restart": service.AutoRestart,
		"last_checked": service.LastChecked,
	}}
	_, err = coll.UpdateOne(ctx, filter, update)
	return err
}

func (r *MongoRepository) BulkUpsertServices(ctx context.Context, services []models.Service) error {
	if len(services) == 0 {
		return nil
	}

	coll := r.db.Collection("services")
	var modelsList []mongo.WriteModel

	for _, svc := range services {
		filter := bson.M{"institution_id": svc.InstitutionID, "name": svc.Name}

		statusStr := svc.Status
		if statusStr == "" {
			statusStr = "UP"
		}

		svcID := svc.ID
		if svcID == "" {
			svcID = bson.NewObjectID().Hex()
		}

		update := bson.M{
			"$set": bson.M{
				"type":         svc.Type,
				"status":       statusStr,
				"auto_restart": svc.AutoRestart,
				"last_checked": svc.LastChecked,
			},
			"$setOnInsert": bson.M{
				"_id": svcID,
			},
		}

		model := mongo.NewUpdateOneModel().SetFilter(filter).SetUpdate(update).SetUpsert(true)
		modelsList = append(modelsList, model)
	}

	opts := options.BulkWrite().SetOrdered(false)
	_, err := coll.BulkWrite(ctx, modelsList, opts)
	return err
}

func (r *MongoRepository) UpdateServiceStatus(ctx context.Context, instID string, name, status string, t time.Time) error {
	coll := r.db.Collection("services")
	filter := bson.M{"institution_id": instID, "name": name}
	update := bson.M{"$set": bson.M{
		"status":       status,
		"last_checked": t,
	}}
	result, err := coll.UpdateOne(ctx, filter, update)
	if err != nil {
		return err
	}
	if result.MatchedCount == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *MongoRepository) UpdateServiceAutoRestart(ctx context.Context, instID, name string, autoRestart bool) error {
	coll := r.db.Collection("services")
	filter := bson.M{"institution_id": instID, "name": name}
	update := bson.M{"$set": bson.M{"auto_restart": autoRestart}}
	result, err := coll.UpdateOne(ctx, filter, update)
	if err != nil {
		return err
	}
	if result.MatchedCount == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *MongoRepository) UpdateServiceRestartStatus(ctx context.Context, instID, name, restartStatus string) error {
	coll := r.db.Collection("services")
	filter := bson.M{"institution_id": instID, "name": name}
	update := bson.M{"$set": bson.M{"restart_status": restartStatus}}
	_, err := coll.UpdateOne(ctx, filter, update)
	return err
}

func (r *MongoRepository) CountServicesByStatus(instID string, status string) (int64, error) {
	coll := r.db.Collection("services")
	return coll.CountDocuments(context.TODO(), bson.M{"institution_id": instID, "status": status})
}

func (r *MongoRepository) CountServicesByStatusBulk(instIDs []string) (map[string]map[string]int64, error) {
	if len(instIDs) == 0 {
		return map[string]map[string]int64{}, nil
	}

	coll := r.db.Collection("services")
	pipeline := mongo.Pipeline{
		{{Key: "$match", Value: bson.M{"institution_id": bson.M{"$in": instIDs}}}},
		{{Key: "$group", Value: bson.D{
			{Key: "_id", Value: bson.D{
				{Key: "institution_id", Value: "$institution_id"},
				{Key: "status", Value: "$status"},
			}},
			{Key: "cnt", Value: bson.D{{Key: "$sum", Value: 1}}},
		}}},
	}

	cursor, err := coll.Aggregate(context.TODO(), pipeline)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(context.TODO())

	var docs []struct {
		ID struct {
			InstitutionID string `bson:"institution_id"`
			Status        string `bson:"status"`
		} `bson:"_id"`
		Cnt int64 `bson:"cnt"`
	}
	if err := cursor.All(context.TODO(), &docs); err != nil {
		return nil, err
	}

	result := make(map[string]map[string]int64)
	for _, doc := range docs {
		instID := doc.ID.InstitutionID
		if result[instID] == nil {
			result[instID] = make(map[string]int64)
		}
		result[instID][doc.ID.Status] = doc.Cnt
	}
	return result, nil
}

func (r *MongoRepository) CreateService(service *models.Service) error {
	coll := r.db.Collection("services")
	if service.ID == "" {
		service.ID = bson.NewObjectID().Hex()
	}
	_, err := coll.InsertOne(context.TODO(), service)
	return err
}

func (r *MongoRepository) DeleteService(ctx context.Context, instID, name string) error {
	coll := r.db.Collection("services")
	filter := bson.M{"institution_id": instID, "name": name}
	result, err := coll.DeleteOne(ctx, filter)
	if err != nil {
		return err
	}
	if result.DeletedCount == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *MongoRepository) Ping(ctx context.Context) error {
	return r.db.Client().Ping(ctx, nil)
}

// --- AuditLog ---

func (r *MongoRepository) CreateAuditLog(ctx context.Context, log *models.AuditLog) error {
	coll := r.db.Collection("audit_logs")
	if log.ID == "" {
		log.ID = bson.NewObjectID().Hex()
	}
	_, err := coll.InsertOne(ctx, log)
	return err
}

func (r *MongoRepository) PurgeAuditLogs(ctx context.Context, olderThan time.Time) (int64, error) {
	coll := r.db.Collection("audit_logs")
	filter := bson.M{
		"$or": []bson.M{
			{"created_at": bson.M{"$lt": olderThan}},
			{"timestamp": bson.M{"$lt": olderThan}},
		},
	}
	res, err := coll.DeleteMany(ctx, filter)
	if err != nil {
		return 0, err
	}
	return res.DeletedCount, nil
}

// --- SecurityLog ---

func (r *MongoRepository) CreateSecurityLog(ctx context.Context, log *models.SecurityLog) error {
	coll := r.db.Collection("security_logs")
	if log.ID == "" {
		log.ID = bson.NewObjectID().Hex()
	}
	_, err := coll.InsertOne(ctx, log)
	return err
}

func (r *MongoRepository) PurgeSecurityLogs(ctx context.Context, olderThan time.Time) (int64, error) {
	coll := r.db.Collection("security_logs")
	filter := bson.M{"timestamp": bson.M{"$lt": olderThan}}
	res, err := coll.DeleteMany(ctx, filter)
	if err != nil {
		return 0, err
	}
	return res.DeletedCount, nil
}

// --- Auth ---

func (r *MongoRepository) GetInstitutionByAgentIDAndSecret(ctx context.Context, agentID, secretKey string) (*models.Institution, error) {
	coll := r.db.Collection("institutions")
	var doc mongoInstitutionDoc
	err := coll.FindOne(ctx, bson.M{"agent_id": agentID, "secret_key": secretKey}).Decode(&doc)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return doc.toModel(), nil
}

// --- User ---

func (r *MongoRepository) GetUserByEmail(email string) (*models.User, error) {
	coll := r.db.Collection("users")
	var doc struct {
		ID           interface{} `bson:"_id"`
		Email        string      `bson:"email"`
		PasswordHash string      `bson:"password_hash"`
		Role         string      `bson:"role"`
		CreatedAt    *time.Time  `bson:"created_at,omitempty"`
	}
	err := coll.FindOne(context.TODO(), bson.M{"email": email}).Decode(&doc)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return nil, ErrNotFound
		}
		return nil, err
	}
	var docID string
	if oid, ok := doc.ID.(bson.ObjectID); ok {
		docID = oid.Hex()
	} else if s, ok := doc.ID.(string); ok {
		docID = s
	}
	user := models.User{
		ID:           docID,
		Email:        doc.Email,
		PasswordHash: doc.PasswordHash,
		Role:         doc.Role,
		CreatedAt:    doc.CreatedAt,
	}
	return &user, nil
}

func (r *MongoRepository) CreateUser(user *models.User) error {
	coll := r.db.Collection("users")
	if user.ID == "" {
		user.ID = bson.NewObjectID().Hex()
	}
	_, err := coll.InsertOne(context.TODO(), user)
	return err
}

func (r *MongoRepository) GetUserByID(ctx context.Context, id string) (*models.User, error) {
	var doc struct {
		ID           string     `bson:"_id"`
		Email        string     `bson:"email"`
		PasswordHash string     `bson:"password_hash"`
		Role         string     `bson:"role"`
		CreatedAt    *time.Time `bson:"created_at,omitempty"`
	}
	err := r.db.Collection("users").FindOne(ctx, bson.M{"_id": id}).Decode(&doc)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return nil, ErrNotFound
		}
		return nil, err
	}
	user := models.User{
		ID:           doc.ID,
		Email:        doc.Email,
		PasswordHash: doc.PasswordHash,
		Role:         doc.Role,
		CreatedAt:    doc.CreatedAt,
	}
	return &user, nil
}

func (r *MongoRepository) UpdateUserPassword(ctx context.Context, id string, hash string) error {
	_, err := r.db.Collection("users").UpdateOne(ctx, bson.M{"_id": id}, bson.M{"$set": bson.M{"password_hash": hash}})
	return err
}

func (r *MongoRepository) GetAuditLogs(ctx context.Context, filter map[string]interface{}, page, limit int) ([]models.AuditLog, int64, error) {
	coll := r.db.Collection("audit_logs")

	// Separate sort/search from filter
	matchFilter := bson.M{}
	searchFilter := bson.M{}
	var hasSearch bool
	sortField := "timestamp"
	sortDir := -1

	for k, v := range filter {
		switch k {
		case "$search":
			if s, ok := v.(string); ok && s != "" {
				hasSearch = true
				searchFilter = bson.M{
					"$or": []bson.M{
						{"service_name": bson.M{"$regex": s, "$options": "i"}},
						{"triggered_by": bson.M{"$regex": s, "$options": "i"}},
						{"details": bson.M{"$regex": s, "$options": "i"}},
						{"action": bson.M{"$regex": s, "$options": "i"}},
					},
				}
			}
		case "$sort_field":
			if sf, ok := v.(string); ok && sf != "" {
				switch sf {
				case "service_name", "action", "status", "triggered_by", "institution_name":
					sortField = sf
				}
			}
		case "$sort_dir":
			if sd, ok := v.(string); ok && strings.ToUpper(sd) == "ASC" {
				sortDir = 1
			}
		default:
			matchFilter[k] = v
		}
	}

	if hasSearch {
		for k, v := range searchFilter {
			matchFilter[k] = v
		}
	}

	total, err := coll.CountDocuments(ctx, matchFilter)
	if err != nil {
		return nil, 0, err
	}

	skip := (page - 1) * limit

	pipeline := mongo.Pipeline{
		{{Key: "$match", Value: matchFilter}},
		{{Key: "$sort", Value: bson.D{{Key: sortField, Value: sortDir}}}},
		{{Key: "$skip", Value: skip}},
		{{Key: "$limit", Value: limit}},
		{{Key: "$lookup", Value: bson.D{
			{Key: "from", Value: "institutions"},
			{Key: "localField", Value: "institution_id"},
			{Key: "foreignField", Value: "_id"},
			{Key: "as", Value: "institution_info"},
		}}},
		{{Key: "$unwind", Value: bson.D{
			{Key: "path", Value: "$institution_info"},
			{Key: "preserveNullAndEmptyArrays", Value: true},
		}}},
		{{Key: "$addFields", Value: bson.D{
			{Key: "institution_name", Value: "$institution_info.name"},
		}}},
		{{Key: "$project", Value: bson.D{
			{Key: "institution_info", Value: 0},
		}}},
	}

	cursor, err := coll.Aggregate(ctx, pipeline)
	if err != nil {
		return nil, 0, err
	}
	defer cursor.Close(ctx)

	var docs []struct {
		ID              interface{} `bson:"_id"`
		InstitutionID   interface{} `bson:"institution_id"`
		InstitutionName string      `bson:"institution_name,omitempty"`
		ServiceName     string      `bson:"service_name"`
		Action          string      `bson:"action"`
		Status          string      `bson:"status"`
		TriggeredBy     string      `bson:"triggered_by"`
		Details         string      `bson:"details,omitempty"`
		Timestamp       time.Time   `bson:"timestamp"`
		CreatedAt       time.Time   `bson:"created_at"`
	}
	if err := cursor.All(ctx, &docs); err != nil {
		return nil, 0, err
	}

	var logs []models.AuditLog
	for _, doc := range docs {
		var docID string
		if oid, ok := doc.ID.(bson.ObjectID); ok {
			docID = oid.Hex()
		} else if s, ok := doc.ID.(string); ok {
			docID = s
		}

		instIDStr := ""
		if oid, ok := doc.InstitutionID.(bson.ObjectID); ok {
			instIDStr = oid.Hex()
		} else if s, ok := doc.InstitutionID.(string); ok {
			instIDStr = s
		}

		logs = append(logs, models.AuditLog{
			ID:              docID,
			InstitutionID:   instIDStr,
			InstitutionName: doc.InstitutionName,
			ServiceName:     doc.ServiceName,
			Action:          doc.Action,
			Status:          doc.Status,
			TriggeredBy:     doc.TriggeredBy,
			Details:         doc.Details,
			Timestamp:       doc.Timestamp,
			CreatedAt:       doc.CreatedAt,
		})
	}

	if logs == nil {
		logs = []models.AuditLog{}
	}
	return logs, total, nil
}

func (r *MongoRepository) GetAuditLogsAll(ctx context.Context, filter map[string]interface{}) ([]models.AuditLog, error) {
	coll := r.db.Collection("audit_logs")

	matchFilter := bson.M{}
	for k, v := range filter {
		switch k {
		case "$search":
			if s, ok := v.(string); ok && s != "" {
				matchFilter["$or"] = []bson.M{
					{"service_name": bson.M{"$regex": s, "$options": "i"}},
					{"triggered_by": bson.M{"$regex": s, "$options": "i"}},
					{"details": bson.M{"$regex": s, "$options": "i"}},
					{"action": bson.M{"$regex": s, "$options": "i"}},
				}
			}
		default:
			matchFilter[k] = v
		}
	}

	pipeline := mongo.Pipeline{
		{{Key: "$match", Value: matchFilter}},
		{{Key: "$sort", Value: bson.D{{Key: "timestamp", Value: -1}}}},
		{{Key: "$limit", Value: 50000}},
		{{Key: "$lookup", Value: bson.D{
			{Key: "from", Value: "institutions"},
			{Key: "localField", Value: "institution_id"},
			{Key: "foreignField", Value: "_id"},
			{Key: "as", Value: "institution_info"},
		}}},
		{{Key: "$unwind", Value: bson.D{
			{Key: "path", Value: "$institution_info"},
			{Key: "preserveNullAndEmptyArrays", Value: true},
		}}},
		{{Key: "$addFields", Value: bson.D{
			{Key: "institution_name", Value: "$institution_info.name"},
		}}},
		{{Key: "$project", Value: bson.D{
			{Key: "institution_info", Value: 0},
		}}},
	}

	cursor, err := coll.Aggregate(ctx, pipeline)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var docs []struct {
		ID              interface{} `bson:"_id"`
		InstitutionID   interface{} `bson:"institution_id"`
		InstitutionName string      `bson:"institution_name,omitempty"`
		ServiceName     string      `bson:"service_name"`
		Action          string      `bson:"action"`
		Status          string      `bson:"status"`
		TriggeredBy     string      `bson:"triggered_by"`
		Details         string      `bson:"details,omitempty"`
		Timestamp       time.Time   `bson:"timestamp"`
		CreatedAt       time.Time   `bson:"created_at"`
	}
	if err := cursor.All(ctx, &docs); err != nil {
		return nil, err
	}

	var logs []models.AuditLog
	for _, doc := range docs {
		var docID string
		if oid, ok := doc.ID.(bson.ObjectID); ok {
			docID = oid.Hex()
		} else if s, ok := doc.ID.(string); ok {
			docID = s
		}

		instIDStr := ""
		if oid, ok := doc.InstitutionID.(bson.ObjectID); ok {
			instIDStr = oid.Hex()
		} else if s, ok := doc.InstitutionID.(string); ok {
			instIDStr = s
		}

		logs = append(logs, models.AuditLog{
			ID:              docID,
			InstitutionID:   instIDStr,
			InstitutionName: doc.InstitutionName,
			ServiceName:     doc.ServiceName,
			Action:          doc.Action,
			Status:          doc.Status,
			TriggeredBy:     doc.TriggeredBy,
			Details:         doc.Details,
			Timestamp:       doc.Timestamp,
			CreatedAt:       doc.CreatedAt,
		})
	}

	if logs == nil {
		logs = []models.AuditLog{}
	}
	return logs, nil
}

func (r *MongoRepository) GetSecurityLogs(ctx context.Context, page, limit int) ([]models.SecurityLog, int64, error) {
	coll := r.db.Collection("security_logs")

	total, err := coll.CountDocuments(ctx, bson.M{})
	if err != nil {
		return nil, 0, err
	}

	skip := (page - 1) * limit

	pipeline := mongo.Pipeline{
		{{Key: "$sort", Value: bson.D{{Key: "timestamp", Value: -1}}}},
		{{Key: "$skip", Value: skip}},
		{{Key: "$limit", Value: limit}},
	}

	cursor, err := coll.Aggregate(ctx, pipeline)
	if err != nil {
		return nil, 0, err
	}
	defer cursor.Close(ctx)

	var docs []struct {
		ID        interface{} `bson:"_id"`
		IP        string      `bson:"ip"`
		Path      string      `bson:"path"`
		Method    string      `bson:"method"`
		UserAgent string      `bson:"user_agent"`
		Timestamp time.Time   `bson:"timestamp"`
	}
	if err := cursor.All(ctx, &docs); err != nil {
		return nil, 0, err
	}

	var logs []models.SecurityLog
	for _, doc := range docs {
		var docID string
		if oid, ok := doc.ID.(bson.ObjectID); ok {
			docID = oid.Hex()
		} else if s, ok := doc.ID.(string); ok {
			docID = s
		}
		logs = append(logs, models.SecurityLog{
			ID:        docID,
			IP:        doc.IP,
			Path:      doc.Path,
			Method:    doc.Method,
			UserAgent: doc.UserAgent,
			Timestamp: doc.Timestamp,
		})
	}

	if logs == nil {
		logs = []models.SecurityLog{}
	}
	return logs, total, nil
}

// --- Settings ---

func (r *MongoRepository) GetSettings(ctx context.Context) (*models.GlobalSetting, error) {
	var doc struct {
		ID                   interface{} `bson:"_id"`
		TelegramBotToken     string      `bson:"telegram_bot_token"`
		TelegramChatID       string      `bson:"telegram_chat_id"`
		TelegramTopicID      string      `bson:"telegram_topic_id"`
		EnableTelegramAlerts bool        `bson:"enable_telegram_alerts"`
		EnableDailyDigest    bool        `bson:"enable_daily_digest"`
		DailyDigestTime      string      `bson:"daily_digest_time"`
		UpdatedAt            time.Time   `bson:"updated_at"`
	}
	err := r.db.Collection("settings").FindOne(ctx, bson.M{}).Decode(&doc)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return &models.GlobalSetting{}, nil
		}
		return nil, err
	}
	var docID string
	if oid, ok := doc.ID.(bson.ObjectID); ok {
		docID = oid.Hex()
	} else if s, ok := doc.ID.(string); ok {
		docID = s
	}
	setting := models.GlobalSetting{
		ID:                   docID,
		TelegramBotToken:     doc.TelegramBotToken,
		TelegramChatID:       doc.TelegramChatID,
		TelegramTopicID:      doc.TelegramTopicID,
		EnableTelegramAlerts: doc.EnableTelegramAlerts,
		EnableDailyDigest:    doc.EnableDailyDigest,
		DailyDigestTime:      doc.DailyDigestTime,
		UpdatedAt:            doc.UpdatedAt,
	}
	return &setting, nil
}

func (r *MongoRepository) UpdateSettings(ctx context.Context, setting *models.GlobalSetting) error {
	update := bson.M{
		"$set": bson.M{
			"telegram_bot_token":     setting.TelegramBotToken,
			"telegram_chat_id":       setting.TelegramChatID,
			"telegram_topic_id":      setting.TelegramTopicID,
			"enable_telegram_alerts": setting.EnableTelegramAlerts,
			"enable_daily_digest":    setting.EnableDailyDigest,
			"daily_digest_time":      setting.DailyDigestTime,
			"updated_at":             setting.UpdatedAt,
		},
	}
	opts := options.UpdateOne().SetUpsert(true)
	_, err := r.db.Collection("settings").UpdateOne(ctx, bson.M{}, update, opts)
	return err
}

// --- Blacklist ---

func (r *MongoRepository) BlacklistIP(ctx context.Context, ip string, reason string) error {
	coll := r.db.Collection("blacklisted_ips")
	filter := bson.M{"ip": ip}
	update := bson.M{
		"$setOnInsert": bson.M{
			"_id":        bson.NewObjectID().Hex(),
			"created_at": time.Now().Local(),
		},
		"$set": bson.M{
			"ip":     ip,
			"reason": reason,
		},
	}
	opts := options.UpdateOne().SetUpsert(true)
	_, err := coll.UpdateOne(ctx, filter, update, opts)
	return err
}

func (r *MongoRepository) IsIPBlacklisted(ctx context.Context, ip string) (bool, error) {
	coll := r.db.Collection("blacklisted_ips")
	count, err := coll.CountDocuments(ctx, bson.M{"ip": ip})
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

func (r *MongoRepository) GetBlacklistedIPs(ctx context.Context) ([]string, error) {
	coll := r.db.Collection("blacklisted_ips")
	cursor, err := coll.Find(ctx, bson.M{})
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var ips []string
	for cursor.Next(ctx) {
		var item models.BlacklistedIP
		if err := cursor.Decode(&item); err == nil && item.IP != "" {
			ips = append(ips, item.IP)
		}
	}
	if ips == nil {
		ips = []string{}
	}
	return ips, nil
}

// --- Schedule ---

// ScheduleRepo returns this repository as a ScheduleRepository.
func (r *MongoRepository) ScheduleRepo() ScheduleRepository {
	return r
}

func (r *MongoRepository) CreateSchedule(ctx context.Context, schedule *models.Schedule) error {
	coll := r.db.Collection("schedules")
	if schedule.ID == "" {
		schedule.ID = bson.NewObjectID().Hex()
	}
	now := time.Now()
	schedule.CreatedAt = now
	schedule.UpdatedAt = now
	_, err := coll.InsertOne(ctx, schedule)
	return err
}

func (r *MongoRepository) GetSchedule(ctx context.Context, id string) (*models.Schedule, error) {
	coll := r.db.Collection("schedules")
	var schedule models.Schedule
	err := coll.FindOne(ctx, bson.M{"_id": id}).Decode(&schedule)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &schedule, nil
}

func (r *MongoRepository) ListSchedules(ctx context.Context, institutionID string, enabledOnly bool) ([]models.Schedule, error) {
	coll := r.db.Collection("schedules")
	filter := bson.M{}
	if institutionID != "" {
		filter["institution_id"] = institutionID
	}
	if enabledOnly {
		filter["enabled"] = true
	}
	opts := options.Find().SetSort(bson.D{{Key: "created_at", Value: -1}})
	cursor, err := coll.Find(ctx, filter, opts)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)
	var schedules []models.Schedule
	if err := cursor.All(ctx, &schedules); err != nil {
		return nil, err
	}
	if schedules == nil {
		schedules = []models.Schedule{}
	}
	return schedules, nil
}

func (r *MongoRepository) UpdateSchedule(ctx context.Context, schedule *models.Schedule) error {
	coll := r.db.Collection("schedules")
	schedule.UpdatedAt = time.Now()
	filter := bson.M{"_id": schedule.ID}
	_, err := coll.ReplaceOne(ctx, filter, schedule)
	return err
}

func (r *MongoRepository) DeleteSchedule(ctx context.Context, id string) error {
	coll := r.db.Collection("schedules")
	filter := bson.M{"_id": id}
	_, err := coll.DeleteOne(ctx, filter)
	return err
}

func (r *MongoRepository) CreateScheduleRun(ctx context.Context, run *models.ScheduleRun) error {
	coll := r.db.Collection("schedule_runs")
	if run.ID == "" {
		run.ID = bson.NewObjectID().Hex()
	}
	run.CreatedAt = time.Now()
	_, err := coll.InsertOne(ctx, run)
	return err
}

func (r *MongoRepository) ListScheduleRuns(ctx context.Context, scheduleID string, page, limit int) ([]models.ScheduleRun, int64, error) {
	coll := r.db.Collection("schedule_runs")
	filter := bson.M{"schedule_id": scheduleID}
	total, err := coll.CountDocuments(ctx, filter)
	if err != nil {
		return nil, 0, err
	}
	opts := options.Find().
		SetSort(bson.D{{Key: "run_at", Value: -1}}).
		SetSkip(int64((page - 1) * limit)).
		SetLimit(int64(limit))
	cursor, err := coll.Find(ctx, filter, opts)
	if err != nil {
		return nil, 0, err
	}
	defer cursor.Close(ctx)
	var runs []models.ScheduleRun
	if err := cursor.All(ctx, &runs); err != nil {
		return nil, 0, err
	}
	if runs == nil {
		runs = []models.ScheduleRun{}
	}
	return runs, total, nil
}

func (r *MongoRepository) GetEnabledSchedules(ctx context.Context) ([]models.Schedule, error) {
	coll := r.db.Collection("schedules")
	filter := bson.M{"enabled": true}
	cursor, err := coll.Find(ctx, filter)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)
	var schedules []models.Schedule
	if err := cursor.All(ctx, &schedules); err != nil {
		return nil, err
	}
	if schedules == nil {
		schedules = []models.Schedule{}
	}
	return schedules, nil
}

func (r *MongoRepository) UpdateScheduleRunTimes(ctx context.Context, scheduleID string, lastRunAt, nextRunAt time.Time) error {
	coll := r.db.Collection("schedules")
	filter := bson.M{"_id": scheduleID}

	setFields := bson.M{}
	if !lastRunAt.IsZero() {
		setFields["last_run_at"] = lastRunAt
	}
	if !nextRunAt.IsZero() {
		setFields["next_run_at"] = nextRunAt
	}
	if len(setFields) == 0 {
		return nil
	}

	update := bson.M{"$set": setFields}
	_, err := coll.UpdateOne(ctx, filter, update)
	return err
}

func (r *MongoRepository) PurgeScheduleRuns(ctx context.Context, olderThan time.Time) (int64, error) {
	coll := r.db.Collection("schedule_runs")
	filter := bson.M{
		"$or": []bson.M{
			{"created_at": bson.M{"$lt": olderThan}},
			{"run_at": bson.M{"$lt": olderThan}},
		},
	}
	res, err := coll.DeleteMany(ctx, filter)
	if err != nil {
		return 0, err
	}
	return res.DeletedCount, nil
}

// CreateIncident records a new incident in MongoDB.
func (r *MongoRepository) CreateIncident(ctx context.Context, inc *models.IncidentLog) error {
	if inc.ID == "" {
		inc.ID = bson.NewObjectID().Hex()
	}
	if inc.CreatedAt.IsZero() {
		inc.CreatedAt = time.Now()
	}
	coll := r.db.Collection("incidents")
	_, err := coll.InsertOne(ctx, inc)
	return err
}

// ResolveActiveIncident resolves an open incident for an institution service/node.
func (r *MongoRepository) ResolveActiveIncident(ctx context.Context, instID, serviceName, incType string, resolvedAt time.Time, resolvedByAuto bool) (*models.IncidentLog, error) {
	coll := r.db.Collection("incidents")
	filter := bson.M{
		"institution_id": instID,
		"type":           incType,
		"resolved_at":    nil,
	}
	if serviceName != "" {
		filter["service_name"] = serviceName
	}
	opts := options.FindOne().SetSort(bson.M{"started_at": -1})
	var inc models.IncidentLog
	err := coll.FindOne(ctx, filter, opts).Decode(&inc)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return nil, nil
		}
		return nil, err
	}

	inc.ResolvedAt = &resolvedAt
	inc.ResolvedByAuto = resolvedByAuto
	inc.DurationSec = int64(resolvedAt.Sub(inc.StartedAt).Seconds())
	if inc.DurationSec < 0 {
		inc.DurationSec = 0
	}

	objID, _ := parseObjectID(inc.ID)
	updateFilter := bson.M{"$or": []bson.M{{"_id": inc.ID}, {"_id": objID}}}
	updateDoc := bson.M{"$set": bson.M{
		"resolved_at":      inc.ResolvedAt,
		"resolved_by_auto": inc.ResolvedByAuto,
		"duration_sec":     inc.DurationSec,
	}}
	_, err = coll.UpdateOne(ctx, updateFilter, updateDoc)
	if err != nil {
		return nil, err
	}
	return &inc, nil
}

// GetIncidentsByInstitution retrieves recent incidents for an institution.
func (r *MongoRepository) GetIncidentsByInstitution(ctx context.Context, instID string, limit int) ([]models.IncidentLog, error) {
	if limit <= 0 {
		limit = 50
	}
	coll := r.db.Collection("incidents")
	opts := options.Find().SetSort(bson.M{"started_at": -1}).SetLimit(int64(limit))
	cur, err := coll.Find(ctx, bson.M{"institution_id": instID}, opts)
	if err != nil {
		return nil, err
	}
	defer cur.Close(ctx)

	var list []models.IncidentLog
	if err := cur.All(ctx, &list); err != nil {
		return nil, err
	}
	if list == nil {
		list = []models.IncidentLog{}
	}
	return list, nil
}

// GetRecentIncidentsCount returns count of incidents started since a timestamp.
func (r *MongoRepository) GetRecentIncidentsCount(ctx context.Context, since time.Time) (int64, error) {
	coll := r.db.Collection("incidents")
	return coll.CountDocuments(ctx, bson.M{"started_at": bson.M{"$gte": since}})
}

// PurgeIncidents deletes incident logs older than a cutoff date.
func (r *MongoRepository) PurgeIncidents(ctx context.Context, olderThan time.Time) (int64, error) {
	coll := r.db.Collection("incidents")
	res, err := coll.DeleteMany(ctx, bson.M{"started_at": bson.M{"$lt": olderThan}})
	if err != nil {
		return 0, err
	}
	return res.DeletedCount, nil
}

// GetUptimeStats calculates daily uptime percentage and incidents count over specified days.
func (r *MongoRepository) GetUptimeStats(ctx context.Context, instID string, days int) (*models.UptimeResponse, error) {
	if days <= 0 {
		days = 30
	}
	now := time.Now().UTC()
	startDate := now.AddDate(0, 0, -days)

	coll := r.db.Collection("incidents")
	cur, err := coll.Find(ctx, bson.M{
		"institution_id": instID,
		"started_at":     bson.M{"$gte": startDate},
	})
	var incidents []models.IncidentLog
	if err == nil {
		_ = cur.All(ctx, &incidents)
		cur.Close(ctx)
	}

	buckets := make([]models.UptimeDayBucket, days)
	var totalDowntimeSec int64

	for i := 0; i < days; i++ {
		dayDate := now.AddDate(0, 0, -(days - 1 - i))
		dateStr := dayDate.Format("2006-01-02")
		startOfDay := time.Date(dayDate.Year(), dayDate.Month(), dayDate.Day(), 0, 0, 0, 0, time.UTC)
		endOfDay := startOfDay.Add(24 * time.Hour)

		var dayIncidents int
		var dayDowntimeSec int64

		for _, inc := range incidents {
			incStart := inc.StartedAt
			incEnd := now
			if inc.ResolvedAt != nil {
				incEnd = *inc.ResolvedAt
			}
			if incStart.Before(endOfDay) && incEnd.After(startOfDay) {
				dayIncidents++
				overlapStart := incStart
				if overlapStart.Before(startOfDay) {
					overlapStart = startOfDay
				}
				overlapEnd := incEnd
				if overlapEnd.After(endOfDay) {
					overlapEnd = endOfDay
				}
				overlapSec := int64(overlapEnd.Sub(overlapStart).Seconds())
				if overlapSec > 0 {
					dayDowntimeSec += overlapSec
				}
			}
		}

		if dayDowntimeSec > 86400 {
			dayDowntimeSec = 86400
		}
		uptimePct := 100.0 - (float64(dayDowntimeSec) / 86400.0 * 100.0)
		if uptimePct < 0 {
			uptimePct = 0
		}

		buckets[i] = models.UptimeDayBucket{
			Date:          dateStr,
			UptimePercent: float64(int(uptimePct*100)) / 100,
			Incidents:     dayIncidents,
			DowntimeSec:   dayDowntimeSec,
		}
		totalDowntimeSec += dayDowntimeSec
	}

	totalSeconds := int64(days) * 86400
	overallUptime := 100.0 - (float64(totalDowntimeSec) / float64(totalSeconds) * 100.0)
	if overallUptime < 0 {
		overallUptime = 0
	}

	return &models.UptimeResponse{
		SLAPercentage: float64(int(overallUptime*100)) / 100,
		TotalDays:     days,
		Days:          buckets,
	}, nil
}
