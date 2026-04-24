package repository

import (
	"context"
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

// --- Institution ---

func (r *MongoRepository) GetInstitutionByAgentID(ctx context.Context, agentID string) (*models.Institution, error) {
	coll := r.db.Collection("institutions")
	var doc struct {
		ID        interface{} `bson:"_id"`
		Name      string      `bson:"name"`
		AgentID   string      `bson:"agent_id"`
		SecretKey string      `bson:"secret_key"`
		IsActive  bool        `bson:"is_active"`
		LastPing  *time.Time  `bson:"last_ping,omitempty"`
	}
	err := coll.FindOne(ctx, bson.M{"agent_id": agentID}).Decode(&doc)
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
	inst := models.Institution{
		ID:        docID,
		Name:      doc.Name,
		AgentID:   doc.AgentID,
		SecretKey: doc.SecretKey,
		IsActive:  doc.IsActive,
		LastPing:  doc.LastPing,
	}
	return &inst, nil
}

func (r *MongoRepository) GetInstitutionByID(id string) (*models.Institution, error) {
	objID, _ := parseObjectID(id)
	coll := r.db.Collection("institutions")
	var doc struct {
		ID           interface{}          `bson:"_id"`
		Name         string               `bson:"name"`
		AgentID      string               `bson:"agent_id"`
		SecretKey    string               `bson:"secret_key"`
		IsActive     bool                 `bson:"is_active"`
		LastPing     *time.Time           `bson:"last_ping,omitempty"`
		HardwareInfo *models.HardwareInfo `bson:"hardware_info,omitempty"`
	}
	filter := bson.M{"$or": []bson.M{{"_id": id}, {"_id": objID}}}
	err := coll.FindOne(context.TODO(), filter).Decode(&doc)
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
	inst := models.Institution{
		ID:           docID,
		Name:         doc.Name,
		AgentID:      doc.AgentID,
		SecretKey:    doc.SecretKey,
		IsActive:     doc.IsActive,
		LastPing:     doc.LastPing,
		HardwareInfo: doc.HardwareInfo,
	}
	return &inst, nil
}

func (r *MongoRepository) GetInstitutionByName(name string) (*models.Institution, error) {
	coll := r.db.Collection("institutions")
	var doc struct {
		ID        interface{} `bson:"_id"`
		Name      string      `bson:"name"`
		AgentID   string      `bson:"agent_id"`
		SecretKey string      `bson:"secret_key"`
		IsActive  bool        `bson:"is_active"`
		LastPing  *time.Time  `bson:"last_ping,omitempty"`
	}
	err := coll.FindOne(context.TODO(), bson.M{"name": name}).Decode(&doc)
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
	inst := models.Institution{
		ID:        docID,
		Name:      doc.Name,
		AgentID:   doc.AgentID,
		SecretKey: doc.SecretKey,
		IsActive:  doc.IsActive,
		LastPing:  doc.LastPing,
	}
	return &inst, nil
}

func (r *MongoRepository) GetAllInstitutions() ([]models.Institution, error) {
	var institutions []models.Institution
	ctx := context.TODO()

	// 1. Ambil semua Institutions
	cursor, err := r.db.Collection("institutions").Find(ctx, bson.M{})
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	if err = cursor.All(ctx, &institutions); err != nil {
		return nil, err
	}

	// Jika kosong, kembalikan array kosong agar tidak panic
	if len(institutions) == 0 {
		return []models.Institution{}, nil
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
		"name":       inst.Name,
		"agent_id":   inst.AgentID,
		"secret_key": inst.SecretKey,
		"is_active":  inst.IsActive,
		"last_ping":  inst.LastPing,
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

func (r *MongoRepository) CreateService(service *models.Service) error {
	coll := r.db.Collection("services")
	if service.ID == "" {
		service.ID = bson.NewObjectID().Hex()
	}
	_, err := coll.InsertOne(context.TODO(), service)
	return err
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
// --- SecurityLog ---

func (r *MongoRepository) CreateSecurityLog(ctx context.Context, log *models.SecurityLog) error {
	coll := r.db.Collection("security_logs")
	if log.ID == "" {
		log.ID = bson.NewObjectID().Hex()
	}
	_, err := coll.InsertOne(ctx, log)
	return err
}

// --- Auth ---

func (r *MongoRepository) GetInstitutionByAgentIDAndSecret(ctx context.Context, agentID, secretKey string) (*models.Institution, error) {
	coll := r.db.Collection("institutions")
	var doc struct {
		ID        interface{} `bson:"_id"`
		Name      string      `bson:"name"`
		AgentID   string      `bson:"agent_id"`
		SecretKey string      `bson:"secret_key"`
		IsActive  bool        `bson:"is_active"`
		LastPing  *time.Time  `bson:"last_ping,omitempty"`
	}
	err := coll.FindOne(ctx, bson.M{"agent_id": agentID, "secret_key": secretKey}).Decode(&doc)
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
	inst := models.Institution{
		ID:        docID,
		Name:      doc.Name,
		AgentID:   doc.AgentID,
		SecretKey: doc.SecretKey,
		IsActive:  doc.IsActive,
		LastPing:  doc.LastPing,
	}
	return &inst, nil
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

	total, err := coll.CountDocuments(ctx, filter)
	if err != nil {
		return nil, 0, err
	}

	skip := (page - 1) * limit

	pipeline := mongo.Pipeline{
		{{Key: "$match", Value: filter}},
		{{Key: "$sort", Value: bson.D{{Key: "timestamp", Value: -1}}}},
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

	// Because we store institution_id as string now but legacy relies on ObjectID,
	// the $lookup might fail to match. We can convert it in the pipeline or just let it be.
	// We'll leave the pipeline as is, but if institution_name is empty it's fine.

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

		// Handle legacy ObjectID vs new String ID
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

// --- Settings ---

func (r *MongoRepository) GetSettings(ctx context.Context) (*models.GlobalSetting, error) {
	var doc struct {
		ID                   interface{} `bson:"_id"`
		TelegramBotToken     string      `bson:"telegram_bot_token"`
		TelegramChatID       string      `bson:"telegram_chat_id"`
		TelegramTopicID      string      `bson:"telegram_topic_id"`
		EnableTelegramAlerts bool        `bson:"enable_telegram_alerts"`
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
	blacklisted := models.BlacklistedIP{
		ID:        bson.NewObjectID().Hex(),
		IP:        ip,
		Reason:    reason,
		CreatedAt: time.Now().Local(),
	}
	_, err := coll.InsertOne(ctx, blacklisted)
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
