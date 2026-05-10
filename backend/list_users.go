package main

import (
	"context"
	"database/sql"
	"fmt"
	"log"

	_ "github.com/lib/pq"
)

func main() {
	// Use the same credentials as in config.yaml
	dsn := "host=localhost port=5432 user=sub2api password=sub2api dbname=sub2api sslmode=disable"
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		log.Fatalf("Failed to open DB: %v", err)
	}
	defer db.Close()

	rows, err := db.QueryContext(context.Background(), "SELECT email, role FROM users")
	if err != nil {
		log.Fatalf("Query failed: %v", err)
	}
	defer rows.Close()

	fmt.Println("Users in database:")
	for rows.Next() {
		var email, role string
		if err := rows.Scan(&email, &role); err != nil {
			log.Fatalf("Scan failed: %v", err)
		}
		fmt.Printf("- %s (%s)\n", email, role)
	}
}
