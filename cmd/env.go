package cmd

import "os"

func firstEnv(keys ...string) string {
	for _, key := range keys {
		if value, ok := os.LookupEnv(key); ok {
			return value
		}
	}

	return ""
}