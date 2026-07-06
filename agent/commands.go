package main

import (
	"encoding/json"
	"os"
	"time"
)

// cmdPing sert de test de connectivité basique.
func cmdPing(params json.RawMessage) (interface{}, error) {
	return map[string]interface{}{
		"pong": true,
		"time": time.Now().Format(time.RFC3339),
	}, nil
}

// cmdGetHostname renvoie le nom de la machine, pour confirmer l'identité
// du poste auquel on est connecté.
func cmdGetHostname(params json.RawMessage) (interface{}, error) {
	name, err := os.Hostname()
	if err != nil {
		return nil, err
	}
	return map[string]interface{}{"hostname": name}, nil
}

// cmdGetUptime renvoie l'uptime réel du système (depuis le démarrage de
// Windows), via GetTickCount64.
func cmdGetUptime(params json.RawMessage) (interface{}, error) {
	ticks, _, _ := procGetTickCount64.Call()
	return map[string]interface{}{"uptime_seconds": float64(ticks) / 1000.0}, nil
}
