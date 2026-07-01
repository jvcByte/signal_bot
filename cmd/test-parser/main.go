package main

import (
	"fmt"
	"os"

	"signal-bot/internal/parser"
)

func main() {
	// Test signal from Mexy Binary
	testSignal := `Mexy Binary
TRADE NOW!!

AUD/USD (OTC)
Timeframe: 2-min expiry
AI Confidence: 80%

Entry Window: 11:08 PM
Direction: BUY

Martingale Levels:
• Level 1 → 11:10 PM
• Level 2 → 11:12 PM
• Level 3 → 11:14 PM`

	fmt.Println("Testing Mexy Binary Signal Parser")
	fmt.Println("==================================\n")
	fmt.Println("Input Signal:")
	fmt.Println(testSignal)
	fmt.Println("\n----------------------------------\n")

	p := parser.New()
	signal, err := p.Parse(testSignal)
	
	if err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: Failed to parse signal: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("✓ Signal parsed successfully!\n")
	fmt.Printf("Asset:      %s\n", signal.Asset)
	fmt.Printf("Direction:  %s\n", signal.Direction)
	fmt.Printf("Expiry:     %d seconds\n", signal.Expiry)
	fmt.Printf("Confidence: %.0f%%\n", signal.Confidence*100)
	fmt.Printf("Raw:        %s...\n", signal.Raw[:50])

	fmt.Println("\n----------------------------------\n")
	fmt.Println("Testing detailed Mexy parser...")
	
	mexySignal, err := parser.ParseMexyDetailed(testSignal)
	if err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: Failed to parse detailed signal: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("✓ Detailed signal parsed!\n")
	fmt.Printf("Entry Window:      %s\n", mexySignal.EntryWindow.Format("3:04 PM"))
	fmt.Printf("Martingale Levels: %d\n", len(mexySignal.MartingaleLevels))
	for _, level := range mexySignal.MartingaleLevels {
		fmt.Printf("  Level %d → %s\n", level.Level, level.Time.Format("3:04 PM"))
	}
	fmt.Printf("\nShould execute now: %v\n", mexySignal.ShouldExecuteNow())

	fmt.Println("\n✓ All tests passed!")
}
