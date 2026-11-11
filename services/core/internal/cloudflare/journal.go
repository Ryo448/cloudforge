package cloudflare

import "encoding/json"

func decodeJournal(data string, out any) error { return json.Unmarshal([]byte(data), out) }
