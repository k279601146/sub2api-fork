package main

import (
	"context"
	"database/sql"
	"log"

	_ "github.com/lib/pq"
	"golang.org/x/crypto/bcrypt"
)

func main() {
	// Use the credentials found in D:\app\data\config.yaml
	dsn := "host=localhost port=5432 user=postgres password=a4875784 dbname=sub2api sslmode=disable"
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		log.Fatalf("Failed to open DB: %v", err)
	}
	defer db.Close()

	email := "admin@sub2api.local"
	password := "admin123"

	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		log.Fatalf("Failed to hash password: %v", err)
	}

	ctx := context.Background()

	// 1. Delete existing if any (including those with the same email)
	_, err = db.ExecContext(ctx, "DELETE FROM users WHERE email = $1", email)
	if err != nil {
		log.Fatalf("Failed to delete old admin: %v", err)
	}

	// 2. Insert new admin
	_, err = db.ExecContext(ctx, `
		INSERT INTO users (email, password_hash, role, balance, concurrency, status, created_at, updated_at)
		VALUES ($1, $2, 'admin', 0, 10, 'active', NOW(), NOW())
	`, email, string(hash))

	if err != nil {
		log.Fatalf("Failed to insert admin: %v", err)
	}

	log.Printf("Admin account %s has been created/reset with password %s", email, password)
}
