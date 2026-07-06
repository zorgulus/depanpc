package main

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

type killProcessParams struct {
	PID int `json:"pid"`
}

// cmdKillProcess termine un processus par son PID via "taskkill". Le PID
// est validé (entier positif) avant d'être passé en argument séparé à
// exec.Command : aucune interprétation shell, aucune injection possible
// même si la valeur reçue était malveillante.
func cmdKillProcess(params json.RawMessage) (interface{}, error) {
	var p killProcessParams
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, fmt.Errorf("params invalides: %w", err)
	}
	if p.PID <= 0 {
		return nil, fmt.Errorf("pid invalide: %d", p.PID)
	}

	out, err := exec.Command("taskkill", "/PID", strconv.Itoa(p.PID), "/F").CombinedOutput()
	text := strings.TrimSpace(decodeOEM(out))
	if err != nil {
		return nil, fmt.Errorf("taskkill a échoué: %v (%s)", err, text)
	}
	return map[string]interface{}{"pid": p.PID, "output": text}, nil
}

// cmdFlushDNS vide le cache DNS local, une action de dépannage réseau
// courante et sans risque.
func cmdFlushDNS(params json.RawMessage) (interface{}, error) {
	out, err := exec.Command("ipconfig", "/flushdns").CombinedOutput()
	text := strings.TrimSpace(decodeOEM(out))
	if err != nil {
		return nil, fmt.Errorf("ipconfig /flushdns a échoué: %v (%s)", err, text)
	}
	return map[string]interface{}{"output": text}, nil
}
