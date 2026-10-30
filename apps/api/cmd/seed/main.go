package main

import (
	"context"
	"database/sql"
	"errors"
	"forma/api/internal/auth"
	"forma/api/internal/config"
	"forma/api/internal/database"
	"forma/api/internal/store"
	"log"
	"os"
	"time"
)

func main() {
	c, err := config.Load()
	if err != nil {
		log.Fatal(err)
	}
	if c.Production {
		log.Fatal("demo seed is disabled in production")
	}
	password := os.Getenv("DEMO_PASSWORD")
	if len(password) < 12 || len(password) > 128 {
		log.Fatal("set DEMO_PASSWORD to 12–128 bytes")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	db, err := database.Open(ctx, c.DatabaseURL)
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()
	s := &store.Store{DB: db}
	_, _, err = s.FindUser(ctx, "hello@forma.test")
	if err == nil {
		log.Print("Demo account already exists; no data changed")
		return
	}
	if !errors.Is(err, sql.ErrNoRows) {
		log.Fatal(err)
	}
	hash, err := auth.HashPassword(password)
	if err != nil {
		log.Fatal(err)
	}
	now := time.Now()
	projects := []store.Project{
		{Name: "Website redesign", Description: "A fresh home for everything we’re building.", Status: "In progress", Color: "violet"},
		{Name: "Mobile experience", Description: "Bring a little more delight to the everyday.", Status: "In progress", Color: "blue"},
		{Name: "Brand guidelines", Description: "One shared language. A thousand possibilities.", Status: "Completed", Color: "amber"},
		{Name: "Customer insights", Description: "Turn good conversations into better decisions.", Status: "In progress", Color: "rose"},
		{Name: "Design system", Description: "Thoughtful foundations for what comes next.", Status: "Planned", Color: "emerald"},
		{Name: "Product launch", Description: "Make our next chapter a memorable one.", Status: "Planned", Color: "violet"},
	}
	for i := range projects {
		projects[i].CreatedAt = now.AddDate(0, 0, -(14 + i))
		projects[i].UpdatedAt = now.Add(-time.Duration(i*4) * time.Hour)
	}
	if err = s.CreateDemoAccount(ctx, "Alex Morgan", "hello@forma.test", hash, projects); err != nil {
		log.Fatal(err)
	}
	log.Print("Created demo account hello@forma.test and six projects")
}
