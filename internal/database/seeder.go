package database

import (
	"context"
	"log"
	"strings"
	"time"

	"monitoring-master/internal/models"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"golang.org/x/crypto/bcrypt"
)

// SeedDatabase inserts default institution data if the collection is empty.
func SeedDatabase(db *mongo.Database) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	coll := db.Collection("institutions")

	count, err := coll.CountDocuments(ctx, bson.M{})
	if err != nil {
		log.Printf("SeedDatabase: failed to count institutions: %v", err)
	}

	if count == 0 {
		log.Println("SeedDatabase: inserting dummy data...")

		// Default institution as requested
		inst := models.Institution{
			ID:        bson.NewObjectID().Hex(),
			Name:      "CUKK Utama",
			AgentID:   "agent-cukk-01",
			SecretKey: "secret-cukk-xyz",
			IsActive:  true,
		}

		_, err = coll.InsertOne(ctx, inst)
		if err != nil {
			log.Printf("SeedDatabase: failed to insert default institution: %v", err)
		} else {
			log.Printf("SeedDatabase: default institution created successfully with ID %s", inst.ID)
		}
	} else {
		log.Println("SeedDatabase: institutions table already has data, skipping seed.")
	}

	// Seed or Update admin user
	usersColl := db.Collection("users")
	var adminUser models.User
	err = usersColl.FindOne(ctx, bson.M{"email": "admin@monitoring.local"}).Decode(&adminUser)

	if err != nil {
		if err == mongo.ErrNoDocuments {
			// Insert admin user
			insertAdminUser(ctx, usersColl)
		} else if strings.Contains(err.Error(), "cannot decode") {
			log.Println("SeedDatabase: corrupted admin user detected (legacy ID type). Deleting and recreating...")
			_, delErr := usersColl.DeleteOne(ctx, bson.M{"email": "admin@monitoring.local"})
			if delErr != nil {
				log.Printf("SeedDatabase: failed to delete corrupted admin user: %v", delErr)
			} else {
				insertAdminUser(ctx, usersColl)
			}
		} else {
			log.Printf("SeedDatabase: error checking admin user: %v", err)
		}
	} else {
		// Admin user exists, check if the password needs to be re-hashed
		// A valid bcrypt hash starts with "$2a$", "$2b$", or "$2y$"
		if len(adminUser.PasswordHash) < 60 || (adminUser.PasswordHash[0:4] != "$2a$" && adminUser.PasswordHash[0:4] != "$2b$" && adminUser.PasswordHash[0:4] != "$2y$") {
			log.Println("SeedDatabase: admin user password is not hashed. Updating to Bcrypt hash...")
			hash, hashErr := bcrypt.GenerateFromPassword([]byte("admin123"), bcrypt.DefaultCost)
			if hashErr == nil {
				_, updateErr := usersColl.UpdateOne(ctx, bson.M{"_id": adminUser.ID}, bson.M{"$set": bson.M{"password_hash": string(hash)}})
				if updateErr != nil {
					log.Printf("SeedDatabase: failed to update admin password: %v", updateErr)
				} else {
					log.Println("SeedDatabase: admin password successfully updated to Bcrypt hash.")
				}
			}
		} else {
			log.Println("SeedDatabase: admin user already exists and has a valid Bcrypt hash.")
		}
	}
}

func insertAdminUser(ctx context.Context, usersColl *mongo.Collection) {
	hash, err := bcrypt.GenerateFromPassword([]byte("admin123"), bcrypt.DefaultCost)
	if err == nil {
		now := time.Now()
		user := models.User{
			ID:           bson.NewObjectID().Hex(),
			Email:        "admin@monitoring.local",
			PasswordHash: string(hash),
			Role:         "admin",
			CreatedAt:    &now,
		}
		_, err = usersColl.InsertOne(ctx, user)
		if err != nil {
			log.Printf("SeedDatabase: failed to insert admin user: %v", err)
		} else {
			log.Println("SeedDatabase: admin user created (admin@monitoring.local / admin123) with Bcrypt hash.")
		}
	} else {
		log.Printf("SeedDatabase: failed to hash admin password: %v", err)
	}
}
