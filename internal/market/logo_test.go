package market

import (
	"strings"
	"testing"
)

func TestAcceptLogo(t *testing.T) {
	png := []byte{0x89, 'P', 'N', 'G', 0x0d, 0x0a, 0x1a, 0x0a}
	jpeg := []byte{0xff, 0xd8, 0xff, 0xe0}
	webp := []byte("RIFF\x00\x00\x00\x00WEBP")
	svg := []byte(`<svg xmlns="http://www.w3.org/2000/svg"></svg>`)

	media, err := AcceptLogo("image/png", png)
	if err != nil || media != "image/png" {
		t.Fatalf("png %s %v", media, err)
	}
	media, err = AcceptLogo("image/jpeg; charset=binary", jpeg)
	if err != nil || media != "image/jpeg" {
		t.Fatalf("jpeg %s %v", media, err)
	}
	media, err = AcceptLogo("image/webp", webp)
	if err != nil || media != "image/webp" {
		t.Fatalf("webp %s %v", media, err)
	}
	media, err = AcceptLogo("image/svg+xml", svg)
	if err != nil || media != "image/svg+xml" {
		t.Fatalf("svg %s %v", media, err)
	}
	media, err = AcceptLogo("application/octet-stream", png)
	if err != nil || media != "image/png" {
		t.Fatalf("sniff %s %v", media, err)
	}
	if _, err := AcceptLogo("text/html", png); err == nil {
		t.Fatal("html type accepted")
	}
	if _, err := AcceptLogo("image/png", jpeg); err == nil {
		t.Fatal("mismatched type accepted")
	}
	if _, err := AcceptLogo("image/svg+xml", []byte(`<svg><script>alert(1)</script></svg>`)); err == nil {
		t.Fatal("script svg accepted")
	}
	if _, err := AcceptLogo("image/png", nil); err == nil {
		t.Fatal("empty accepted")
	}
	big := make([]byte, MaxLogoBytes+1)
	copy(big, png)
	if _, err := AcceptLogo("image/png", big); err == nil {
		t.Fatal("oversize accepted")
	}
}

func TestCryptoLogoURL(t *testing.T) {
	got, ok := CryptoLogoURL("BTC")
	if !ok || !strings.HasSuffix(got, "/btc.png") || !strings.HasPrefix(got, "https://") {
		t.Fatalf("%s %v", got, ok)
	}
	if _, ok := CryptoLogoURL("BAD SYMBOL"); ok {
		t.Fatal("space accepted")
	}
}
