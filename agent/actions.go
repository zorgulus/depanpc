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

// cmdEnableShell ne fait rien par lui-même : le vrai effet (déverrouiller
// run_command pour la connexion en cours) est géré dans handleConfirm
// (main.go), qui a accès à l'état de la connexion contrairement à ce
// handler générique. Cette fonction existe pour que enable_shell apparaisse
// dans la whitelist comme toute autre commande.
func cmdEnableShell(params json.RawMessage) (interface{}, error) {
	return map[string]interface{}{"shell_enabled": true}, nil
}

type runCommandParams struct {
	Command string `json:"command"`
	Shell   string `json:"shell"` // "cmd" (défaut) ou "powershell"
}

// cmdRunCommand exécute une commande arbitraire. Contrairement à toutes les
// autres commandes de la whitelist, la chaîne fournie par le client est
// interprétée par un vrai shell (cmd ou PowerShell) : aucune protection
// contre l'injection n'est possible ni recherchée ici, par choix assumé
// (accès complet type SSH, gardé derrière enable_shell). Une commande qui
// échoue (code de sortie non nul) renvoie quand même sa sortie normalement
// plutôt qu'une erreur protocolaire, comme le ferait un vrai terminal.
func cmdRunCommand(params json.RawMessage) (interface{}, error) {
	var p runCommandParams
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, fmt.Errorf("params invalides: %w", err)
	}
	if strings.TrimSpace(p.Command) == "" {
		return nil, fmt.Errorf("commande vide")
	}

	var cmd *exec.Cmd
	switch p.Shell {
	case "", "cmd":
		cmd = exec.Command("cmd", "/C", p.Command)
	case "powershell":
		cmd = exec.Command("powershell", "-NoProfile", "-NonInteractive", "-Command", p.Command)
	default:
		return nil, fmt.Errorf("shell inconnu: %q (valeurs possibles: cmd, powershell)", p.Shell)
	}

	out, execErr := cmd.CombinedOutput()
	text := strings.TrimSpace(decodeOEM(out))
	result := map[string]interface{}{"output": text}
	if execErr != nil {
		result["exit_error"] = execErr.Error()
	}
	return result, nil
}
