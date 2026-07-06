package main

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"math"
	"net"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"unsafe"
)

// cmdListDisks renvoie l'usage (total/libre/utilisé) de chaque volume monté
// (fixe, amovible ou réseau) via l'API Win32, sans dépendance externe.
func cmdListDisks(params json.RawMessage) (interface{}, error) {
	maskUintptr, _, _ := procGetLogicalDrives.Call()
	mask := uint32(maskUintptr)

	var disks []map[string]interface{}
	for i := 0; i < 26; i++ {
		if mask&(1<<uint(i)) == 0 {
			continue
		}
		letter := string(rune('A' + i))
		root := letter + `:\`
		rootPtr, err := syscall.UTF16PtrFromString(root)
		if err != nil {
			continue
		}

		driveType, _, _ := procGetDriveTypeW.Call(uintptr(unsafe.Pointer(rootPtr)))
		// DRIVE_REMOVABLE=2, DRIVE_FIXED=3, DRIVE_REMOTE=4
		if driveType != 2 && driveType != 3 && driveType != 4 {
			continue
		}

		var freeAvail, total, totalFree uint64
		ret, _, _ := procGetDiskFreeSpaceExW.Call(
			uintptr(unsafe.Pointer(rootPtr)),
			uintptr(unsafe.Pointer(&freeAvail)),
			uintptr(unsafe.Pointer(&total)),
			uintptr(unsafe.Pointer(&totalFree)),
		)
		if ret == 0 || total == 0 {
			continue
		}

		used := total - totalFree
		percent := math.Round(float64(used)/float64(total)*1000) / 10

		disks = append(disks, map[string]interface{}{
			"drive":        letter + ":",
			"total_bytes":  total,
			"free_bytes":   totalFree,
			"used_bytes":   used,
			"used_percent": percent,
		})
	}
	return map[string]interface{}{"disks": disks}, nil
}

// cmdNetworkInfo liste les interfaces réseau et leurs adresses.
func cmdNetworkInfo(params json.RawMessage) (interface{}, error) {
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil, err
	}

	var result []map[string]interface{}
	for _, iface := range ifaces {
		addrs, _ := iface.Addrs()
		var addrStrs []string
		for _, a := range addrs {
			addrStrs = append(addrStrs, a.String())
		}
		result = append(result, map[string]interface{}{
			"name":      iface.Name,
			"mac":       iface.HardwareAddr.String(),
			"up":        iface.Flags&net.FlagUp != 0,
			"addresses": addrStrs,
		})
	}
	return map[string]interface{}{"interfaces": result}, nil
}

type processInfo struct {
	Name     string `json:"name"`
	PID      int    `json:"pid"`
	MemoryKB int    `json:"memory_kb"`
}

// cmdListProcesses s'appuie sur l'utilitaire "tasklist" intégré à Windows
// (commande fixe, sans paramètre venant du client) et trie par mémoire
// utilisée, plafonné à 50 résultats pour éviter une réponse trop lourde.
func cmdListProcesses(params json.RawMessage) (interface{}, error) {
	out, err := exec.Command("tasklist", "/fo", "csv", "/nh").Output()
	if err != nil {
		return nil, err
	}

	reader := csv.NewReader(strings.NewReader(decodeOEM(out)))
	records, err := reader.ReadAll()
	if err != nil {
		return nil, err
	}

	var procs []processInfo
	for _, rec := range records {
		if len(rec) < 5 {
			continue
		}
		pid, _ := strconv.Atoi(rec[1])
		procs = append(procs, processInfo{Name: rec[0], PID: pid, MemoryKB: parseMemoryKB(rec[4])})
	}
	totalCount := len(procs)

	sort.Slice(procs, func(i, j int) bool { return procs[i].MemoryKB > procs[j].MemoryKB })
	const maxReturned = 50
	if len(procs) > maxReturned {
		procs = procs[:maxReturned]
	}

	return map[string]interface{}{"processes": procs, "total_count": totalCount, "shown": len(procs)}, nil
}

func parseMemoryKB(s string) int {
	var digits []rune
	for _, r := range s {
		if r >= '0' && r <= '9' {
			digits = append(digits, r)
		}
	}
	n, _ := strconv.Atoi(string(digits))
	return n
}

type eventLogParams struct {
	Log string `json:"log"`
	Max int    `json:"max"`
}

type eventEntry struct {
	LogName     string `json:"log_name,omitempty"`
	Source      string `json:"source,omitempty"`
	Date        string `json:"date,omitempty"`
	EventID     string `json:"event_id,omitempty"`
	Level       string `json:"level,omitempty"`
	Description string `json:"description,omitempty"`
}

// cmdGetEventLog lit les entrées Critique/Erreur/Avertissement récentes
// d'un journal d'événements Windows via "wevtutil". Le nom du journal est
// restreint à une liste fermée (System, Application) et le nombre
// d'entrées est plafonné : la commande shell reste entièrement déterminée
// par l'agent, jamais construite à partir d'une chaîne libre du client.
func cmdGetEventLog(params json.RawMessage) (interface{}, error) {
	var p eventLogParams
	if len(params) > 0 {
		if err := json.Unmarshal(params, &p); err != nil {
			return nil, fmt.Errorf("params invalides: %w", err)
		}
	}

	logName := p.Log
	if logName == "" {
		logName = "System"
	}
	if logName != "System" && logName != "Application" {
		return nil, fmt.Errorf("log non autorisé: %q (valeurs possibles: System, Application)", logName)
	}

	max := p.Max
	if max <= 0 {
		max = 20
	}
	if max > 50 {
		max = 50
	}

	const query = "*[System[(Level=1 or Level=2 or Level=3)]]"
	out, err := exec.Command("wevtutil", "qe", logName, "/q:"+query, "/c:"+strconv.Itoa(max), "/rd:true", "/f:text").Output()
	if err != nil {
		return nil, err
	}

	events := parseWevtutilText(decodeANSI(out))
	return map[string]interface{}{"log": logName, "events": events}, nil
}

func parseWevtutilText(raw string) []eventEntry {
	lines := strings.Split(strings.ReplaceAll(raw, "\r\n", "\n"), "\n")
	var events []eventEntry
	var current *eventEntry
	var descLines []string
	inDescription := false

	flush := func() {
		if current != nil {
			current.Description = strings.TrimSpace(strings.Join(descLines, "\n"))
			events = append(events, *current)
		}
	}

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "Event[") {
			flush()
			current = &eventEntry{}
			descLines = nil
			inDescription = false
			continue
		}
		if current == nil {
			continue
		}
		switch {
		case strings.HasPrefix(trimmed, "Log Name:"):
			current.LogName = strings.TrimSpace(strings.TrimPrefix(trimmed, "Log Name:"))
			inDescription = false
		case strings.HasPrefix(trimmed, "Source:"):
			current.Source = strings.TrimSpace(strings.TrimPrefix(trimmed, "Source:"))
			inDescription = false
		case strings.HasPrefix(trimmed, "Date:"):
			current.Date = strings.TrimSpace(strings.TrimPrefix(trimmed, "Date:"))
			inDescription = false
		case strings.HasPrefix(trimmed, "Event ID:"):
			current.EventID = strings.TrimSpace(strings.TrimPrefix(trimmed, "Event ID:"))
			inDescription = false
		case strings.HasPrefix(trimmed, "Level:"):
			current.Level = strings.TrimSpace(strings.TrimPrefix(trimmed, "Level:"))
			inDescription = false
		case strings.HasPrefix(trimmed, "Description:"):
			inDescription = true
			rest := strings.TrimSpace(strings.TrimPrefix(trimmed, "Description:"))
			if rest != "" {
				descLines = append(descLines, rest)
			}
		default:
			if inDescription && trimmed != "" {
				descLines = append(descLines, trimmed)
			}
		}
	}
	flush()
	return events
}
