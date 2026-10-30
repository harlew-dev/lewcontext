package main

import (
	"context"
	"errors"
	"forma/api/internal/auth"
	"forma/api/internal/config"
	"forma/api/internal/database"
	"forma/api/internal/store"
	"github.com/jackc/pgx/v5"
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
	if !errors.Is(err, pgx.ErrNoRows) {
		log.Fatal(err)
	}
	hash, err := auth.HashPassword(password)
	if err != nil {
		log.Fatal(err)
	}
	tx, err := db.Begin(ctx)
	if err != nil {
		log.Fatal(err)
	}
	defer tx.Rollback(ctx)
	var id int64
	if err = tx.QueryRow(ctx, "INSERT INTO users(name,email,password_hash) VALUES('Alex Morgan','hello@forma.test',$1) RETURNING id", hash).Scan(&id); err != nil {
		log.Fatal(err)
	}
	projects := []store.ProjectInput{
		{Name: "Website redesign", Description: "A fresh home for everything we’re building.", Status: "In progress", Color: "violet"},
		{Name: "Mobile experience", Description: "Bring a little more delight to the everyday.", Status: "In progress", Color: "blue"},
		{Name: "Brand guidelines", Description: "One shared language. A thousand possibilities.", Status: "Completed", Color: "amber"},
		{Name: "Customer insights", Description: "Turn good conversations into better decisions.", Status: "In progress", Color: "rose"},
		{Name: "Design system", Description: "Thoughtful foundations for what comes next.", Status: "Planned", Color: "emerald"},
		{Name: "Product launch", Description: "Make our next chapter a memorable one.", Status: "Planned", Color: "violet"},
	}
	for i, p := range projects {
		if _, err = tx.Exec(ctx, "INSERT INTO projects(user_id,name,description,status,color,created_at,updated_at) VALUES($1,$2,$3,$4,$5,now()-($6 * interval '1 day'),now()-($7 * interval '1 hour'))", id, p.Name, p.Description, p.Status, p.Color, 14+i, i*4); err != nil {
			log.Fatal(err)
		}
	}
	if err = tx.Commit(ctx); err != nil {
		log.Fatal(err)
	}
	log.Print("Created demo account hello@forma.test and six projects")
}
