package main

import (
	"fmt"
	"log"
	"os"
	"sort"
	"strings"

	"github.com/rs/zerolog"
	"signal-bot/internal/config"
	"signal-bot/internal/wstrader"
)

func main() {
	logger := zerolog.New(zerolog.ConsoleWriter{Out: os.Stdout, TimeFormat: "15:04:05"}).
		With().Timestamp().Logger().Level(zerolog.InfoLevel)

	cfg, err := config.Load("configs/config.yaml")
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	trader := wstrader.New(&cfg.IQOption, logger)
	
	logger.Info().Msg("Connecting to IQ Option...")
	if err := trader.Connect(); err != nil {
		log.Fatalf("Failed to connect: %v", err)
	}
	defer trader.Close()

	logger.Info().Msg("Fetching all available assets...")
	
	assets, err := trader.ListAllAssets()
	if err != nil {
		log.Fatalf("Failed to get assets: %v", err)
	}
	
	logger.Info().Int("total", len(assets)).Msg("Available assets loaded")
	
	// Search for OpenAI-related assets
	fmt.Println("\n=== Searching for OpenAI-related assets ===")
	searchTerms := []string{"OPENAI", "OPEN", "AI", "GPT", "CHATGPT"}
	found := false
	
	for name, id := range assets {
		nameUpper := strings.ToUpper(name)
		for _, term := range searchTerms {
			if strings.Contains(nameUpper, term) {
				fmt.Printf("✓ Found: %s (ID: %d)\n", name, id)
				found = true
				break
			}
		}
	}
	
	if !found {
		fmt.Println("✗ No OpenAI-related assets found")
	}
	
	// List all OTC assets
	fmt.Println("\n=== All OTC Assets ===")
	var otcAssets []string
	for name := range assets {
		if strings.Contains(strings.ToUpper(name), "OTC") {
			otcAssets = append(otcAssets, name)
		}
	}
	sort.Strings(otcAssets)
	
	for _, name := range otcAssets {
		fmt.Printf("  %s (ID: %d)\n", name, assets[name])
	}
	
	fmt.Printf("\nTotal OTC assets: %d\n", len(otcAssets))
}

