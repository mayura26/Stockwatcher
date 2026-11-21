package market

import "strings"

// Indexes is the curated Yahoo list for yields, volatility, and related macro prints.
// Display is the short root. ProviderSymbol is the Yahoo chart symbol.
// Hits use the stock class so they store beside other Yahoo listings.
var Indexes = []Hit{
	{AssetClass: Stock, Display: "IRX", ProviderSymbol: "^IRX", Name: "13-Week Treasury Yield"},
	{AssetClass: Stock, Display: "FVX", ProviderSymbol: "^FVX", Name: "5-Year Treasury Yield"},
	{AssetClass: Stock, Display: "TNX", ProviderSymbol: "^TNX", Name: "10-Year Treasury Yield"},
	{AssetClass: Stock, Display: "TYX", ProviderSymbol: "^TYX", Name: "30-Year Treasury Yield"},
	{AssetClass: Stock, Display: "VIX", ProviderSymbol: "^VIX", Name: "CBOE Volatility Index"},
	{AssetClass: Stock, Display: "VIX3M", ProviderSymbol: "^VIX3M", Name: "CBOE 3-Month Volatility"},
	{AssetClass: Stock, Display: "VVIX", ProviderSymbol: "^VVIX", Name: "CBOE VIX Volatility"},
	{AssetClass: Stock, Display: "DXY", ProviderSymbol: "DX-Y.NYB", Name: "US Dollar Index"},
	{AssetClass: Stock, Display: "MOVE", ProviderSymbol: "^MOVE", Name: "ICE BofA MOVE Index"},
	{AssetClass: Stock, Display: "GSPC", ProviderSymbol: "^GSPC", Name: "S&P 500"},
	{AssetClass: Stock, Display: "DJI", ProviderSymbol: "^DJI", Name: "Dow Jones Industrial Average"},
	{AssetClass: Stock, Display: "IXIC", ProviderSymbol: "^IXIC", Name: "Nasdaq Composite"},
	{AssetClass: Stock, Display: "RUT", ProviderSymbol: "^RUT", Name: "Russell 2000"},
}

// YahooIndex reports a provider that charts as a Yahoo index.
// A caret symbol such as ^TNX, and the dollar index DX-Y.NYB, take that path.
func YahooIndex(provider string) bool {
	provider = strings.ToUpper(strings.TrimSpace(provider))
	return strings.HasPrefix(provider, "^") || provider == "DX-Y.NYB"
}

// SearchIndexes filters the curated list. An unknown symbol is not invented.
func SearchIndexes(q string) []Hit {
	q = strings.ToUpper(strings.TrimSpace(q))
	if q == "" {
		out := make([]Hit, len(Indexes))
		copy(out, Indexes)
		return out
	}
	var out []Hit
	for _, idx := range Indexes {
		if strings.HasPrefix(idx.Display, q) || strings.Contains(strings.ToUpper(idx.Name), q) || strings.HasPrefix(idx.ProviderSymbol, q) {
			out = append(out, idx)
		}
	}
	return out
}
