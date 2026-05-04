package main

import (
	"context"
	"log"
	"net/http"
	"os"

	"bedrock-relay/internal/relay"
)

func main() {
	ctx := context.Background()
	cfg, client, err := relay.LoadAppConfig(ctx, os.Args[1:])
	if err != nil {
		log.Fatal(err)
	}

	server := relay.NewServer(cfg, relay.NewAWSBedrockInvoker(client))
	log.Printf("bedrock-relay listening on :%s using AWS profile %q in %s", cfg.Port, cfg.AWSProfile, cfg.Region)
	if err := http.ListenAndServe(":"+cfg.Port, server.Routes()); err != nil {
		log.Fatal(err)
	}
}
