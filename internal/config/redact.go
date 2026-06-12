package config

import "encoding/json"

func RedactedJSON(cfg Config) ([]byte, error) {
	redacted := cfg
	if redacted.API.Key != "" {
		redacted.API.Key = "[REDACTED]"
	}
	if redacted.Identity.PrivateKey != "" {
		redacted.Identity.PrivateKey = "[REDACTED]"
	}
	for i := range redacted.Identity.Groups {
		if redacted.Identity.Groups[i].Token != "" {
			redacted.Identity.Groups[i].Token = "[REDACTED]"
		}
	}
	for i := range redacted.Peers {
		if redacted.Peers[i].APIKey != "" {
			redacted.Peers[i].APIKey = "[REDACTED]"
		}
	}
	data, err := json.MarshalIndent(redacted, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(data, '\n'), nil
}
