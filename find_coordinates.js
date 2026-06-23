// ═══════════════════════════════════════════════════════════
// IQ OPTION COORDINATE FINDER
// ═══════════════════════════════════════════════════════════
// Paste this into Chrome DevTools Console (F12) on IQ Option page
// Then click on the UI elements to capture their coordinates

(function() {
    console.clear();
    console.log("%c🎯 COORDINATE FINDER ACTIVATED", "color: green; font-size: 16px; font-weight: bold");
    console.log("\n%cClick on these UI elements in order:", "color: blue; font-weight: bold");
    console.log("1️⃣  Asset selector (shows current pair like 'EUR/USD')");
    console.log("2️⃣  Expiry selector (shows timeframe like '2 min')");
    console.log("3️⃣  Amount input field");
    console.log("4️⃣  Green CALL/BUY button");
    console.log("5️⃣  Red PUT/SELL button");
    console.log("─".repeat(60));

    let clickCount = 0;
    const labels = [
        { name: "Asset Selector", yaml: "asset" },
        { name: "Expiry Selector", yaml: "expiry" },
        { name: "Amount Input", yaml: "amount" },
        { name: "CALL Button", yaml: "call" },
        { name: "PUT Button", yaml: "put" }
    ];
    
    const coords = [];

    const handler = function(e) {
        e.stopPropagation();
        
        if (clickCount < 5) {
            const label = labels[clickCount];
            console.log(`\n%c${clickCount + 1}. ${label.name}`, "color: orange; font-weight: bold");
            console.log(`   X: ${e.clientX}, Y: ${e.clientY}`);
            console.log(`   %cYAML: ${label.yaml}_x: ${e.clientX}`, "color: green");
            console.log(`   %cYAML: ${label.yaml}_y: ${e.clientY}`, "color: green");
            
            coords.push({ label: label.yaml, x: e.clientX, y: e.clientY });
            clickCount++;
            
            if (clickCount === 5) {
                console.log("\n" + "═".repeat(60));
                console.log("%c✅ ALL COORDINATES COLLECTED!", "color: green; font-size: 14px; font-weight: bold");
                console.log("%cCopy this to configs/config.yaml:", "color: blue; font-weight: bold");
                console.log("─".repeat(60));
                console.log("coordinates:");
                coords.forEach(c => {
                    console.log(`  ${c.label}_x: ${c.x}`);
                    console.log(`  ${c.label}_y: ${c.y}`);
                });
                console.log("═".repeat(60));
                
                document.removeEventListener('click', handler, true);
            }
        }
    };

    document.addEventListener('click', handler, true);
    
    console.log("%c✨ Ready! Click on the elements now...", "color: green");
})();
