package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	apix "github.com/mnafshin/apix/pkg/api/generated"
	"google.golang.org/grpc"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Println("Usage: apix-cli [status|log|plugins]")
		os.Exit(1)
	}

	// Connect to engine gRPC API
	conn, err := grpc.Dial("localhost:9090", grpc.WithInsecure())
	if err != nil {
		log.Fatalf("failed to connect to engine: %v", err)
	}
	defer conn.Close()
	client := apix.NewEngineClient(conn)

	switch os.Args[1] {
	case "status":
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		resp, err := client.GetStatus(ctx, &apix.StatusRequest{})
		if err != nil {
			log.Fatalf("GetStatus failed: %v", err)
		}
		fmt.Printf("Engine status: %s (version %s)\n", resp.Status, resp.Version)

	case "plugins":
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		resp, err := client.ListPlugins(ctx, &apix.PluginListRequest{})
		if err != nil {
			log.Fatalf("ListPlugins failed: %v", err)
		}
		fmt.Println("Installed plugins:")
		for _, p := range resp.Plugins {
			fmt.Printf(" - %s (%s): %s\n", p.Name, p.Version, p.Description)
		}

	case "log":
		ctx := context.Background()
		stream, err := client.CaptureTraffic(ctx, &apix.CaptureRequest{})
		if err != nil {
			log.Fatalf("CaptureTraffic failed: %v", err)
		}
		fmt.Println("Streaming captured traffic...")
		for {
			req, err := stream.Recv()
			if err != nil {
				log.Fatalf("stream error: %v", err)
			}
			fmt.Printf("[%s] %s %s\n", time.Unix(req.Timestamp, 0), req.Method, req.Url)
		}

	default:
		fmt.Println("Unknown command. Use: status, log, plugins")
	}
}
