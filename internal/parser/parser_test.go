package parser

import (
	"testing"

	"signal-bot/pkg/models"
)

func TestParser_Parse(t *testing.T) {
	tests := []struct {
		name       string
		input      string
		wantAsset  string
		wantDir    models.Direction
		wantExp    int
		wantConf   float64
		wantErr    bool
	}{
		{
			name: "mexy_binary_buy",
			input: `Mexy Binary
TRADE NOW!!

AUD/USD (OTC)
Timeframe: 2-min expiry
AI Confidence: 80%

Entry Window: 11:08 PM
Direction: BUY

Martingale Levels:
• Level 1 → 11:10 PM
• Level 2 → 11:12 PM
• Level 3 → 11:14 PM`,
			wantAsset: "AUDUSD",
			wantDir:   models.DirectionCall,
			wantExp:   2,
			wantConf:  0.8,
			wantErr:   false,
		},
		{
			name: "mexy_binary_sell",
			input: `Mexy Binary
TRADE NOW!!

EUR/USD (OTC)
Timeframe: 5-min expiry
AI Confidence: 75%

Entry Window: 10:00 PM
Direction: SELL

Martingale Levels:
• Level 1 → 10:05 PM`,
			wantAsset: "EURUSD",
			wantDir:   models.DirectionPut,
			wantExp:   5,
			wantConf:  0.75,
			wantErr:   false,
		},
		{
			name:      "pattern1_call",
			input:     "EUR/USD CALL 5MIN",
			wantAsset: "EURUSD",
			wantDir:   models.DirectionCall,
			wantExp:   5,
			wantErr:   false,
		},
		{
			name:      "pattern1_put",
			input:     "GBP/JPY PUT 15MIN",
			wantAsset: "GBPJPY",
			wantDir:   models.DirectionPut,
			wantExp:   15,
			wantErr:   false,
		},
		{
			name:      "pattern2",
			input:     "EURUSD - CALL - 5M",
			wantAsset: "EURUSD",
			wantDir:   models.DirectionCall,
			wantExp:   5,
			wantErr:   false,
		},
		{
			name:      "pattern3_buy",
			input:     "BUY EUR/USD 5 MINUTES",
			wantAsset: "EURUSD",
			wantDir:   models.DirectionCall,
			wantExp:   5,
			wantErr:   false,
		},
		{
			name:      "pattern3_sell",
			input:     "SELL GBP/USD 10 MINUTE",
			wantAsset: "GBPUSD",
			wantDir:   models.DirectionPut,
			wantExp:   10,
			wantErr:   false,
		},
		{
			name:    "invalid",
			input:   "INVALID SIGNAL FORMAT",
			wantErr: true,
		},
	}

	p := New()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := p.Parse(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("Parse() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.wantErr {
				return
			}
			if got.Asset != tt.wantAsset {
				t.Errorf("Asset = %v, want %v", got.Asset, tt.wantAsset)
			}
			if got.Direction != tt.wantDir {
				t.Errorf("Direction = %v, want %v", got.Direction, tt.wantDir)
			}
			if got.Expiry != tt.wantExp {
				t.Errorf("Expiry = %v, want %v", got.Expiry, tt.wantExp)
			}
			if tt.wantConf > 0 && got.Confidence != tt.wantConf {
				t.Errorf("Confidence = %v, want %v", got.Confidence, tt.wantConf)
			}
		})
	}
}
