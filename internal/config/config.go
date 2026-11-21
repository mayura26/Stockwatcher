package config

import "os"

// Config is process configuration. Secrets come from the environment.
type Config struct {
	DBPath          string
	AccessToken     string
	FinnhubKey      string
	VAPIDPublic     string
	VAPIDPrivate    string
	VAPIDSubscriber string
	Addr            string
}

func FromEnv() Config {
	db := os.Getenv("DB_PATH")
	if db == "" {
		db = "data/stockwatcher.db"
	}
	addr := os.Getenv("ADDR")
	if addr == "" {
		addr = ":8080"
	}
	return Config{
		DBPath:          db,
		AccessToken:     os.Getenv("ACCESS_TOKEN"),
		FinnhubKey:      os.Getenv("FINNHUB_API_KEY"),
		VAPIDPublic:     os.Getenv("VAPID_PUBLIC_KEY"),
		VAPIDPrivate:    os.Getenv("VAPID_PRIVATE_KEY"),
		VAPIDSubscriber: os.Getenv("VAPID_SUBSCRIBER"),
		Addr:            addr,
	}
}
