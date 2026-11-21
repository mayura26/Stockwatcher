package market

import (
	"errors"
	"mime"
	"strings"
)

const (
	// MaxLogoBytes is the largest image kept for a symbol mark.
	MaxLogoBytes = 256 << 10
	// StockLogoBase is the public Finnhub logo file, plus "{SYMBOL}.png".
	// Company profile is paid on this key, so the file is fetched directly.
	StockLogoBase = "https://static2.finnhub.io/file/publicdatany/finnhubimage/stock_logo/"
	// CryptoLogoBase is a pinned color PNG for a lowercase ticker, plus ".png".
	CryptoLogoBase = "https://cdn.jsdelivr.net/npm/cryptocurrency-icons@0.18.1/128/color/"
	// ParqetLogoBase covers listings the Finnhub file does not, such as 005930.KS.
	ParqetLogoBase = "https://assets.parqet.com/logos/symbol/"
)

// ErrLogo means the bytes are not a small png, jpeg, webp, or svg.
var ErrLogo = errors.New("logo rejected")

// StockLogoURL is the public PNG for a listing such as NVDA or BRK.B.
// base is StockLogoBase except in tests. A symbol with any other character has no URL.
func StockLogoURL(base, symbol string) (string, bool) {
	sym := strings.ToUpper(strings.TrimSpace(symbol))
	if base == "" || sym == "" || len(sym) > 16 {
		return "", false
	}
	for _, r := range sym {
		if (r < 'A' || r > 'Z') && (r < '0' || r > '9') && r != '.' && r != '-' {
			return "", false
		}
	}
	return base + sym + ".png", true
}

// ParqetLogoURL is a second image for the same listing symbol.
func ParqetLogoURL(symbol string) (string, bool) {
	sym := strings.ToUpper(strings.TrimSpace(symbol))
	if sym == "" || len(sym) > 16 {
		return "", false
	}
	for _, r := range sym {
		if (r < 'A' || r > 'Z') && (r < '0' || r > '9') && r != '.' && r != '-' {
			return "", false
		}
	}
	return ParqetLogoBase + sym, true
}

// CryptoLogoURL is the icon for a crypto display symbol such as BTC.
// A symbol with any other character has no URL.
func CryptoLogoURL(display string) (string, bool) {
	name := strings.ToLower(strings.TrimSpace(display))
	if name == "" || len(name) > 16 {
		return "", false
	}
	for _, r := range name {
		if (r < 'a' || r > 'z') && (r < '0' || r > '9') {
			return "", false
		}
	}
	return CryptoLogoBase + name + ".png", true
}

// AcceptLogo checks the type and the size. The returned type is what to store
// and serve. A generic or empty Content-Type falls back to the byte sniff.
func AcceptLogo(contentType string, body []byte) (string, error) {
	if len(body) == 0 || len(body) > MaxLogoBytes {
		return "", ErrLogo
	}
	media := mediaType(contentType)
	switch media {
	case "image/png", "image/jpeg", "image/webp":
		if sniffLogo(body) != media {
			return "", ErrLogo
		}
		return media, nil
	case "image/svg+xml":
		if !looksLikeSVG(body) {
			return "", ErrLogo
		}
		return media, nil
	case "", "application/octet-stream":
		if sniffed := sniffLogo(body); sniffed != "" {
			return sniffed, nil
		}
		if looksLikeSVG(body) {
			return "image/svg+xml", nil
		}
		return "", ErrLogo
	default:
		return "", ErrLogo
	}
}

func mediaType(header string) string {
	if strings.TrimSpace(header) == "" {
		return ""
	}
	v, _, err := mime.ParseMediaType(header)
	if err != nil {
		return ""
	}
	return strings.ToLower(v)
}

func sniffLogo(body []byte) string {
	if len(body) >= 8 && body[0] == 0x89 && body[1] == 'P' && body[2] == 'N' && body[3] == 'G' {
		return "image/png"
	}
	if len(body) >= 3 && body[0] == 0xff && body[1] == 0xd8 && body[2] == 0xff {
		return "image/jpeg"
	}
	if len(body) >= 12 && string(body[0:4]) == "RIFF" && string(body[8:12]) == "WEBP" {
		return "image/webp"
	}
	return ""
}

func looksLikeSVG(body []byte) bool {
	s := strings.TrimSpace(string(body))
	s = strings.TrimPrefix(s, "\uFEFF")
	lower := strings.ToLower(s)
	if strings.Contains(lower, "<script") {
		return false
	}
	if strings.HasPrefix(lower, "<svg") {
		return true
	}
	return strings.HasPrefix(lower, "<?xml") && strings.Contains(lower, "<svg")
}
