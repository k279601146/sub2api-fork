package main

import (
	"fmt"
	"log"

	"github.com/Wei-Shaw/sub2api/internal/config"
)

func main() {
	cfg, err := config.LoadForBootstrap()
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}
	fmt.Printf("DB Host: %s\n", cfg.Database.Host)
	fmt.Printf("DB User: %s\n", cfg.Database.User)
	fmt.Printf("DB Password: %s\n", cfg.Database.Password)
	fmt.Printf("DB Name: %s\n", cfg.Database.DBName)
}
