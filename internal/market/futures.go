package market

import "strings"

// Futures is the curated CME (and CBOT/NYMEX/COMEX) continuous list on Yahoo.
// Display is the root, provider symbol is the Yahoo continuous contract.
var Futures = []Hit{
	{AssetClass: Future, Display: "ES", ProviderSymbol: "ES=F", Name: "E-mini S&P 500"},
	{AssetClass: Future, Display: "NQ", ProviderSymbol: "NQ=F", Name: "E-mini Nasdaq-100"},
	{AssetClass: Future, Display: "YM", ProviderSymbol: "YM=F", Name: "E-mini Dow"},
	{AssetClass: Future, Display: "RTY", ProviderSymbol: "RTY=F", Name: "E-mini Russell 2000"},
	{AssetClass: Future, Display: "MES", ProviderSymbol: "MES=F", Name: "Micro E-mini S&P 500"},
	{AssetClass: Future, Display: "MNQ", ProviderSymbol: "MNQ=F", Name: "Micro E-mini Nasdaq-100"},
	{AssetClass: Future, Display: "MYM", ProviderSymbol: "MYM=F", Name: "Micro E-mini Dow"},
	{AssetClass: Future, Display: "M2K", ProviderSymbol: "M2K=F", Name: "Micro E-mini Russell 2000"},
	{AssetClass: Future, Display: "CL", ProviderSymbol: "CL=F", Name: "Crude Oil"},
	{AssetClass: Future, Display: "MCL", ProviderSymbol: "MCL=F", Name: "Micro Crude Oil"},
	{AssetClass: Future, Display: "NG", ProviderSymbol: "NG=F", Name: "Natural Gas"},
	{AssetClass: Future, Display: "RB", ProviderSymbol: "RB=F", Name: "RBOB Gasoline"},
	{AssetClass: Future, Display: "HO", ProviderSymbol: "HO=F", Name: "NY Harbor ULSD"},
	{AssetClass: Future, Display: "GC", ProviderSymbol: "GC=F", Name: "Gold"},
	{AssetClass: Future, Display: "MGC", ProviderSymbol: "MGC=F", Name: "Micro Gold"},
	{AssetClass: Future, Display: "SI", ProviderSymbol: "SI=F", Name: "Silver"},
	{AssetClass: Future, Display: "HG", ProviderSymbol: "HG=F", Name: "Copper"},
	{AssetClass: Future, Display: "PL", ProviderSymbol: "PL=F", Name: "Platinum"},
	{AssetClass: Future, Display: "PA", ProviderSymbol: "PA=F", Name: "Palladium"},
	{AssetClass: Future, Display: "ZB", ProviderSymbol: "ZB=F", Name: "30-Year T-Bond"},
	{AssetClass: Future, Display: "ZN", ProviderSymbol: "ZN=F", Name: "10-Year T-Note"},
	{AssetClass: Future, Display: "ZF", ProviderSymbol: "ZF=F", Name: "5-Year T-Note"},
	{AssetClass: Future, Display: "ZT", ProviderSymbol: "ZT=F", Name: "2-Year T-Note"},
	{AssetClass: Future, Display: "UB", ProviderSymbol: "UB=F", Name: "Ultra T-Bond"},
	{AssetClass: Future, Display: "TN", ProviderSymbol: "TN=F", Name: "Ultra 10-Year"},
	{AssetClass: Future, Display: "SR3", ProviderSymbol: "SR3=F", Name: "Three-Month SOFR"},
	{AssetClass: Future, Display: "6E", ProviderSymbol: "6E=F", Name: "Euro FX"},
	{AssetClass: Future, Display: "6J", ProviderSymbol: "6J=F", Name: "Japanese Yen"},
	{AssetClass: Future, Display: "6B", ProviderSymbol: "6B=F", Name: "British Pound"},
	{AssetClass: Future, Display: "6A", ProviderSymbol: "6A=F", Name: "Australian Dollar"},
	{AssetClass: Future, Display: "6C", ProviderSymbol: "6C=F", Name: "Canadian Dollar"},
	{AssetClass: Future, Display: "6S", ProviderSymbol: "6S=F", Name: "Swiss Franc"},
	{AssetClass: Future, Display: "6N", ProviderSymbol: "6N=F", Name: "New Zealand Dollar"},
	{AssetClass: Future, Display: "6M", ProviderSymbol: "6M=F", Name: "Mexican Peso"},
	{AssetClass: Future, Display: "ZC", ProviderSymbol: "ZC=F", Name: "Corn"},
	{AssetClass: Future, Display: "ZS", ProviderSymbol: "ZS=F", Name: "Soybeans"},
	{AssetClass: Future, Display: "ZW", ProviderSymbol: "ZW=F", Name: "Chicago Wheat"},
	{AssetClass: Future, Display: "ZL", ProviderSymbol: "ZL=F", Name: "Soybean Oil"},
	{AssetClass: Future, Display: "ZM", ProviderSymbol: "ZM=F", Name: "Soybean Meal"},
	{AssetClass: Future, Display: "KE", ProviderSymbol: "KE=F", Name: "KC Wheat"},
	{AssetClass: Future, Display: "LE", ProviderSymbol: "LE=F", Name: "Live Cattle"},
	{AssetClass: Future, Display: "HE", ProviderSymbol: "HE=F", Name: "Lean Hogs"},
	{AssetClass: Future, Display: "GF", ProviderSymbol: "GF=F", Name: "Feeder Cattle"},
	{AssetClass: Future, Display: "BTC", ProviderSymbol: "BTC=F", Name: "CME Bitcoin"},
	{AssetClass: Future, Display: "ETH", ProviderSymbol: "ETH=F", Name: "CME Ether"},
	{AssetClass: Future, Display: "MBT", ProviderSymbol: "MBT=F", Name: "Micro Bitcoin"},
}

// SearchFutures filters the curated list and accepts a raw Yahoo symbol such as ES=F.
func SearchFutures(q string) []Hit {
	q = strings.ToUpper(strings.TrimSpace(q))
	if q == "" {
		out := make([]Hit, len(Futures))
		copy(out, Futures)
		return out
	}
	var out []Hit
	seen := map[string]bool{}
	for _, f := range Futures {
		if strings.HasPrefix(f.Display, q) || strings.Contains(strings.ToUpper(f.Name), q) || strings.HasPrefix(f.ProviderSymbol, q) {
			out = append(out, f)
			seen[f.ProviderSymbol] = true
		}
	}
	if strings.HasSuffix(q, "=F") && !seen[q] && validFutureSymbol(q) {
		out = append(out, Hit{
			AssetClass:     Future,
			Display:        strings.TrimSuffix(q, "=F"),
			ProviderSymbol: q,
			Name:           q,
		})
	}
	return out
}

func validFutureSymbol(s string) bool {
	if !strings.HasSuffix(s, "=F") {
		return false
	}
	root := strings.TrimSuffix(s, "=F")
	if root == "" || len(root) > 12 {
		return false
	}
	for _, r := range root {
		if (r < 'A' || r > 'Z') && (r < '0' || r > '9') {
			return false
		}
	}
	return true
}
