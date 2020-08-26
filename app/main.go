package main

import (
	"log"
	"os"

	"github.com/darrenmcc/dizmo"
	remindme "github.com/darrenmcc/remind-me"
)

func main() {
	svc, err := remindme.NewService(
		mustEnv("TO"),
		mustEnv("FROM"),
		mustEnv("SECRET"),
		mustEnv("SENDGRID_SECRET"),
	)
	if err != nil {
		log.Fatalf("unable to create new service: %s", err)
	}

	err = dizmo.Run(svc)
	if err != nil {
		log.Fatalf("unable to start service: %s", err)
	}
}

func mustEnv(k string) string {
	v := os.Getenv(k)
	if v == "" {
		log.Fatalf("%q environment variable not set", k)
	}
	return v
}
