package relay

import (
	"encoding/json"
	"net/http"
)

func writeSSEData(w http.ResponseWriter, flusher http.Flusher, event string, data []byte) {
	if event != "" {
		_, _ = w.Write([]byte("event: "))
		_, _ = w.Write([]byte(event))
		_, _ = w.Write([]byte("\n"))
	}
	_, _ = w.Write([]byte("data: "))
	_, _ = w.Write(data)
	_, _ = w.Write([]byte("\n\n"))
	flusher.Flush()
}

func writeSSEJSON(w http.ResponseWriter, flusher http.Flusher, value any) {
	data, _ := json.Marshal(value)
	writeSSEData(w, flusher, "", data)
}
